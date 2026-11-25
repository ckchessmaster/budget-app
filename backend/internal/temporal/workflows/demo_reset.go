package workflows

import (
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// demoSandboxNamespace namespaces the derived sandbox IDs. It is a fixed constant: the
// sandbox workspace is generated from it plus the workflow ID, so it must be identical on
// every replay and on every retry.
var demoSandboxNamespace = uuid.MustParse("2f2a1c48-6b0e-4c9f-9c1d-8a7f5e0d3b21")

// DemoResetInput is the scope plus whether to sync the fresh sandbox once it exists.
type DemoResetInput struct {
	temporalinternal.SyncScope
	TriggerSync bool `json:"trigger_sync"`
}

type DemoResetResult struct {
	WorkspaceID      string `json:"workspace_id"`
	CategoriesSeeded int    `json:"categories_seeded"`
	AccountsSeeded   int    `json:"accounts_seeded"`
	ConnectionReady  bool   `json:"connection_ready"`
	SyncsCancelled   int    `json:"syncs_cancelled,omitempty"`
}

// DemoResetWorkflow tears the demo sandbox down and rebuilds it.
//
// The order is load-bearing and is expressed here rather than inside one transaction:
//
//  1. Turn demo_mode_enabled on. The workspace policies hide a demo workspace until it is
//     set, so a delete issued first cannot see the rows it is removing.
//  2. Cancel and wait out any bank sync for this workspace. Racing one is how the sandbox
//     ends up with transactions whose connection no longer exists.
//  3. Delete the old sandboxes and their connections, which is also the documented
//     recovery path after a vault key change.
//  4. Seed a fresh sandbox.
//
// The sandbox workspace ID is derived from the workflow ID with SHA-1 rather than
// generated. That keeps it stable across replays, as workflow code requires, and stable
// across activity retries, which is what lets step 4 delete-and-rebuild instead of
// stacking a second sandbox under a new ID every time it runs.
func DemoResetWorkflow(ctx workflow.Context, input DemoResetInput) (*DemoResetResult, error) {
	if input.WorkspaceID == uuid.Nil || input.UserID == uuid.Nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"workspace_id and user_id are required to reset the demo sandbox", "InvalidResetScope", nil)
	}

	info := workflow.GetInfo(ctx)
	sandboxID := uuid.NewSHA1(demoSandboxNamespace, []byte(info.WorkflowExecution.ID))

	quick := workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
		},
	}
	// Seeding writes a whole sandbox, and draining a stubborn sync can wait a minute, so
	// both get more room than the single-statement steps.
	heavy := workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    3 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    1 * time.Minute,
			MaximumAttempts:    4,
		},
	}

	var demoActivities *activities.DemoActivities

	enableCtx := workflow.WithActivityOptions(ctx, quick)
	if err := workflow.ExecuteActivity(enableCtx, demoActivities.EnableDemoModeActivity, input.SyncScope).Get(enableCtx, nil); err != nil {
		return nil, err
	}

	drainCtx := workflow.WithActivityOptions(ctx, heavy)
	var cancelled int
	if err := workflow.ExecuteActivity(drainCtx, demoActivities.CancelWorkspaceSyncsActivity, input.SyncScope).Get(drainCtx, &cancelled); err != nil {
		return nil, err
	}

	deleteCtx := workflow.WithActivityOptions(ctx, quick)
	if err := workflow.ExecuteActivity(deleteCtx, demoActivities.DeleteDemoSandboxActivity, input.SyncScope).Get(deleteCtx, nil); err != nil {
		return nil, err
	}

	seedCtx := workflow.WithActivityOptions(ctx, heavy)
	var seeded *activities.SeedDemoSandboxResult
	if err := workflow.ExecuteActivity(seedCtx, demoActivities.SeedDemoSandboxActivity, activities.SeedDemoSandboxInput{
		SyncScope: input.SyncScope,
		SandboxID: sandboxID,
	}).Get(seedCtx, &seeded); err != nil {
		return nil, err
	}

	result := &DemoResetResult{SyncsCancelled: cancelled}
	if seeded != nil {
		result.WorkspaceID = seeded.WorkspaceID
		result.CategoriesSeeded = seeded.CategoriesSeeded
		result.AccountsSeeded = seeded.AccountsSeeded
		result.ConnectionReady = seeded.ConnectionReady
	}

	if input.TriggerSync && result.WorkspaceID != "" {
		syncScope := temporalinternal.SyncScope{UserID: input.UserID}
		if parsed, err := uuid.Parse(result.WorkspaceID); err == nil {
			syncScope.WorkspaceID = parsed
			syncCtx := workflow.WithActivityOptions(ctx, quick)
			if err := workflow.ExecuteActivity(syncCtx, demoActivities.StartSandboxSyncActivity, syncScope).Get(syncCtx, nil); err != nil {
				// The sandbox is usable without a sync; failing the whole reset over a
				// scheduling hiccup would be worse than reporting it.
				workflow.GetLogger(ctx).Warn("demo sandbox seeded but follow-up sync could not start", "error", err)
			}
		}
	}

	return result, nil
}
