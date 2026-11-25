package activities

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// SandboxSeeder builds a demo sandbox under a workspace ID the caller already knows.
//
// It is a function field rather than a call into internal/demo because that package
// imports the schedule helpers, which import the workflows, which import this package.
// Importing demo from here would close that cycle; the worker binary wires the real
// seeder in, since it already depends on both sides.
type SandboxSeeder func(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	sandboxID uuid.UUID,
	mockBaseURL string,
	vault crypto.Vault,
) (uuid.UUID, error)

// DemoActivities is the demo sandbox teardown and rebuild that used to run inside a
// single POST /api/v1/demo/reset transaction.
//
// It was the slowest request in the app and the one most often interrupted — deploys,
// client cancels and pod evictions all landed here, and every past bug in demo mode (RLS
// scoping, bridge credentials, vault rotation) surfaced as a half-rebuilt sandbox. Split
// into activities, each step is individually retryable and the sequencing that matters is
// expressed in the workflow rather than buried in one transaction.
type DemoActivities struct {
	Pool        *pgxpool.Pool
	Vault       crypto.Vault
	MockBaseURL string
	Temporal    client.Client
	SeedSandbox SandboxSeeder
}

// SeedDemoSandboxInput names the sandbox to build. SandboxID is derived from the workflow
// ID by the workflow itself, so every attempt of this activity rebuilds the same sandbox
// instead of inventing a new one and stacking it on the last.
type SeedDemoSandboxInput struct {
	temporalinternal.SyncScope
	SandboxID uuid.UUID `json:"sandbox_id"`
}

type SeedDemoSandboxResult struct {
	WorkspaceID      string `json:"workspace_id"`
	CategoriesSeeded int    `json:"categories_seeded"`
	AccountsSeeded   int    `json:"accounts_seeded"`
	ConnectionReady  bool   `json:"connection_ready"`
}

// EnableDemoModeActivity flips the preference that makes a workspace visible.
//
// This has to happen before anything is deleted: the workspace policies hide a demo
// workspace until demo_mode_enabled is true, so a delete issued first cannot see the rows
// it is trying to remove. It is a single upsert and safe to repeat.
func (a *DemoActivities) EnableDemoModeActivity(ctx context.Context, scope temporalinternal.SyncScope) error {
	if scope.UserID == uuid.Nil {
		return temporal.NewNonRetryableApplicationError(
			"user_id is required to reset the demo sandbox", "InvalidResetScope", nil)
	}
	return db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_preferences (user_id, demo_mode_enabled, updated_at)
			VALUES ($1, TRUE, NOW())
			ON CONFLICT (user_id) DO UPDATE
			SET demo_mode_enabled = TRUE,
			    updated_at = NOW()
		`, scope.UserID)
		return err
	})
}

// CancelWorkspaceSyncsActivity stops and waits out any bank sync running against the
// sandbox that is about to be torn down.
//
// Racing a sync is how the sandbox ends up with transactions whose connection no longer
// exists. The reset therefore neither ignores a running sync nor fires-and-forgets a
// cancel: it cancels, then blocks until the execution is closed. Cancelling rather than
// refusing the reset keeps reset usable as the escape hatch the demo UI treats it as.
func (a *DemoActivities) CancelWorkspaceSyncsActivity(ctx context.Context, scope temporalinternal.SyncScope) (int, error) {
	if a.Temporal == nil {
		return 0, temporal.NewNonRetryableApplicationError(
			"no Temporal client is available to drain running bank syncs", "TemporalUnavailable", nil)
	}

	// Search attributes are not configured, so the workspace is matched on the workflow
	// ID, which every sync execution embeds. The visibility API is paginated; a demo
	// workspace rarely has more than one or two syncs running, but the loop keeps the
	// step correct if that ever stops being true.
	type execution struct{ id, runID string }
	var targets []execution
	pageToken := []byte(nil)
	for {
		resp, err := a.Temporal.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			Namespace:     temporalinternal.NamespaceFromEnv(),
			PageSize:      100,
			NextPageToken: pageToken,
			Query:         "ExecutionStatus='Running' and WorkflowType='BankSyncWorkflow'",
		})
		if err != nil {
			return 0, fmt.Errorf("failed to list running bank syncs: %w", err)
		}
		for _, exec := range resp.GetExecutions() {
			id := exec.GetExecution().GetWorkflowId()
			if strings.Contains(id, scope.WorkspaceID.String()) {
				targets = append(targets, execution{id: id, runID: exec.GetExecution().GetRunId()})
			}
		}
		if len(resp.GetNextPageToken()) == 0 {
			break
		}
		pageToken = resp.GetNextPageToken()
	}

	for _, wf := range targets {
		// A cancel on an execution that closed between listing and here is expected and
		// harmless; the wait below is what actually proves it is gone.
		if cancelErr := a.Temporal.CancelWorkflow(ctx, wf.id, wf.runID); cancelErr != nil {
			continue
		}
	}

	for _, wf := range targets {
		waitCtx, cancelWait := context.WithTimeout(ctx, 60*time.Second)
		// Every outcome here is acceptable: cancelled, failed or completed all mean the
		// execution stopped writing. A wait that itself times out is the one case worth
		// reporting, because the sandbox is then about to be removed under a sync that
		// refused to stop.
		getErr := a.Temporal.GetWorkflow(waitCtx, wf.id, wf.runID).Get(waitCtx, nil)
		timedOut := errors.Is(waitCtx.Err(), context.DeadlineExceeded)
		cancelWait()
		if timedOut {
			return len(targets), fmt.Errorf("bank sync %s did not stop within 60s of cancellation", wf.id)
		}
		_ = getErr
	}

	return len(targets), nil
}

// DeleteDemoSandboxActivity removes the user's demo workspaces.
//
// Connections are cleared explicitly before the workspace goes away. The foreign key
// would cascade anyway, but saying it out loud makes this the documented recovery path
// after a VAULT_MASTER_KEY change: rows sealed with the old key are dropped rather than
// left undecryptable behind a workspace that no longer resolves.
func (a *DemoActivities) DeleteDemoSandboxActivity(ctx context.Context, scope temporalinternal.SyncScope) (int, error) {
	deleted := 0
	err := db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT w.id
			FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE wm.user_id = $1 AND w.is_demo = TRUE
		`, scope.UserID)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, id := range ids {
			if _, err := tx.Exec(ctx, `
				DELETE FROM simplefin_connections WHERE workspace_id = $1
			`, id); err != nil {
				return err
			}
		}

		tag, err := tx.Exec(ctx, `
			DELETE FROM workspaces
			WHERE id IN (
				SELECT w.id
				FROM workspaces w
				JOIN workspace_members wm ON wm.workspace_id = w.id
				WHERE wm.user_id = $1 AND w.is_demo = TRUE
			)
		`, scope.UserID)
		if err != nil {
			return err
		}
		deleted = int(tag.RowsAffected())
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// SeedDemoSandboxActivity builds a fresh sandbox under SandboxID.
//
// It is idempotent by construction: whatever already sits under that key is deleted
// first, so a retry after a partial failure rebuilds cleanly instead of duplicating
// accounts, transactions or the connection row.
func (a *DemoActivities) SeedDemoSandboxActivity(
	ctx context.Context,
	input SeedDemoSandboxInput,
) (*SeedDemoSandboxResult, error) {
	if input.UserID == uuid.Nil || input.SandboxID == uuid.Nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"user_id and sandbox_id are required to seed a demo sandbox", "InvalidResetScope", nil)
	}
	if a.SeedSandbox == nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"no demo sandbox seeder is configured", "SeederUnavailable", nil)
	}

	result := &SeedDemoSandboxResult{WorkspaceID: input.SandboxID.String()}
	err := db.WithWorkspaceTx(ctx, a.Pool, input.UserID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			DELETE FROM simplefin_connections WHERE workspace_id = $1
		`, input.SandboxID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM workspaces WHERE id = $1 AND is_demo = TRUE
		`, input.SandboxID); err != nil {
			return err
		}

		seededID, err := a.SeedSandbox(ctx, tx, input.UserID, input.SandboxID, a.MockBaseURL, a.Vault)
		if err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM categories WHERE workspace_id = $1
		`, seededID).Scan(&result.CategoriesSeeded); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM accounts WHERE workspace_id = $1
		`, seededID).Scan(&result.AccountsSeeded); err != nil {
			return err
		}
		var connections int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM simplefin_connections WHERE workspace_id = $1
		`, seededID).Scan(&connections); err != nil {
			return err
		}
		result.ConnectionReady = connections == 1
		result.WorkspaceID = seededID.String()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// StartSandboxSyncActivity queues a first bank sync for a freshly seeded sandbox.
//
// It starts the workflow by name rather than by function reference: BankSyncWorkflow lives
// in the workflows package, which imports this one. The name has to match the registration
// in backend/cmd/worker/main.go, and the input mirrors BankSyncInput, which is a bare
// embedded SyncScope.
func (a *DemoActivities) StartSandboxSyncActivity(ctx context.Context, scope temporalinternal.SyncScope) error {
	if a.Temporal == nil {
		return temporal.NewNonRetryableApplicationError(
			"no Temporal client is available to start a sandbox sync", "TemporalUnavailable", nil)
	}
	if scope.WorkspaceID == uuid.Nil || scope.UserID == uuid.Nil {
		return temporal.NewNonRetryableApplicationError(
			"workspace_id and user_id are required to sync a demo sandbox", "InvalidResetScope", nil)
	}

	_, err := a.Temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("bank-sync-manual-%s-%d", scope.WorkspaceID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, "BankSyncWorkflow", struct {
		WorkspaceID uuid.UUID `json:"workspace_id"`
		UserID      uuid.UUID `json:"user_id"`
	}{WorkspaceID: scope.WorkspaceID, UserID: scope.UserID})
	if err != nil {
		return fmt.Errorf("failed to start the post-reset bank sync: %w", err)
	}
	return nil
}
