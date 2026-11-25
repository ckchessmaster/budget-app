package activities_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/demo"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openDemoResetTestDB(t *testing.T) (adminPool, appPool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	appURL := os.Getenv("TEST_DATABASE_URL")
	if appURL == "" {
		appURL = adminURL
	}

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("skipping demo reset test (admin pool error): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping demo reset test (postgres unreachable): %v", err)
	}
	appPool, err = pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping demo reset test (app pool error): %v", err)
	}
	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})
	return adminPool, appPool
}

func newDemoUser(t *testing.T, adminPool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	userID, wsID := uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Demo Reset WS')", wsID); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM user_preferences WHERE user_id = $1", userID)
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE user_id IS NULL AND id IN (SELECT id FROM workspaces WHERE is_demo = TRUE AND id IN (SELECT workspace_id FROM workspace_members WHERE user_id = $1))", userID)
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE id = $1", wsID)
		_, _ = adminPool.Exec(bg, "DELETE FROM workspace_members WHERE user_id = $1", userID)
	})
	return userID, wsID
}

func demoVault(t *testing.T) crypto.Vault {
	t.Helper()
	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build vault: %v", err)
	}
	return vault
}

// TestSeedDemoSandboxActivityIsIdempotent is the core resumability claim: the reset can be
// interrupted and replayed, and the sandbox must come out the same shape rather than
// doubled. Row counts after two runs must equal the counts after one.
func TestSeedDemoSandboxActivityIsIdempotent(t *testing.T) {
	adminPool, appPool := openDemoResetTestDB(t)
	ctx := context.Background()
	userID, wsID := newDemoUser(t, adminPool)

	acts := &activities.DemoActivities{
		Pool:        appPool,
		Vault:       demoVault(t),
		MockBaseURL: "http://budget-app-budget-app-backend-bridge:8090",
		SeedSandbox: demo.SeedDemoWorkspaceTxWithID,
	}
	// Demo mode has to be on before anything can be seeded: the workspace policies hide a
	// demo workspace until it is, and seeding without it fails on workspace_settings. The
	// workflow does this first for exactly that reason.
	if err := acts.EnableDemoModeActivity(ctx, temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}); err != nil {
		t.Fatalf("enable demo mode: %v", err)
	}

	// The same derivation the workflow and the status endpoint apply.
	sandboxID := temporalinternal.DemoSandboxID(temporalinternal.DemoResetWorkflowIDPrefix + wsID.String())
	input := activities.SeedDemoSandboxInput{
		SyncScope: temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID},
		SandboxID: sandboxID,
	}

	first, err := acts.SeedDemoSandboxActivity(ctx, input)
	if err != nil {
		t.Fatalf("first seed failed: %v", err)
	}
	second, err := acts.SeedDemoSandboxActivity(ctx, input)
	if err != nil {
		t.Fatalf("replayed seed failed: %v", err)
	}

	if first.WorkspaceID != second.WorkspaceID {
		t.Fatalf("replay built a different sandbox: %s then %s", first.WorkspaceID, second.WorkspaceID)
	}
	if second.CategoriesSeeded != first.CategoriesSeeded || second.AccountsSeeded != first.AccountsSeeded {
		t.Fatalf("replay changed row counts: (%d,%d) then (%d,%d)",
			first.CategoriesSeeded, first.AccountsSeeded, second.CategoriesSeeded, second.AccountsSeeded)
	}
	if second.CategoriesSeeded == 0 || second.AccountsSeeded == 0 {
		t.Fatalf("sandbox seeded nothing: %+v", second)
	}
	if !second.ConnectionReady {
		t.Fatalf("replayed sandbox has no single ready connection: %+v", second)
	}

	var wsCount, connCount, txCount int
	if err := adminPool.QueryRow(ctx, `SELECT COUNT(*) FROM workspaces WHERE id = $1`, sandboxID).Scan(&wsCount); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if wsCount != 1 {
		t.Fatalf("sandbox workspaces = %d, want exactly 1", wsCount)
	}
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM simplefin_connections WHERE workspace_id = $1
	`, sandboxID).Scan(&connCount); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if connCount != 1 {
		t.Fatalf("simplefin_connections rows = %d, want exactly 1", connCount)
	}
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transactions t
		JOIN accounts a ON a.id = t.account_id
		WHERE a.workspace_id = $1
	`, sandboxID).Scan(&txCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txCount == 0 {
		t.Fatalf("sandbox has no starter transactions")
	}
}

// TestEnableDemoModePrecedesDeletion pins the ordering rule. The workspace policies hide a
// demo workspace until demo_mode_enabled is true, so a delete issued first cannot see the
// rows it is removing and silently deletes nothing.
func TestEnableDemoModePrecedesDeletion(t *testing.T) {
	adminPool, appPool := openDemoResetTestDB(t)
	ctx := context.Background()
	userID, wsID := newDemoUser(t, adminPool)

	acts := &activities.DemoActivities{
		Pool:        appPool,
		Vault:       demoVault(t),
		MockBaseURL: "http://bridge:8090",
		SeedSandbox: demo.SeedDemoWorkspaceTxWithID,
	}
	scope := temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}

	if err := acts.EnableDemoModeActivity(ctx, scope); err != nil {
		t.Fatalf("enable demo mode: %v", err)
	}

	// Build a sandbox the same way the workflow would, then confirm the delete can see
	// and remove it now that the preference is on.
	sandboxID := uuid.New()
	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, err := demo.SeedDemoWorkspaceTxWithID(ctx, tx, userID, sandboxID, "http://bridge:8090", acts.Vault)
		return err
	})
	if err != nil {
		t.Fatalf("seed sandbox: %v", err)
	}

	deleted, err := acts.DeleteDemoSandboxActivity(ctx, scope)
	if err != nil {
		t.Fatalf("delete sandbox: %v", err)
	}
	if deleted == 0 {
		t.Fatalf("delete removed nothing; demo_mode_enabled ordering is load-bearing")
	}

	var remaining int
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM simplefin_connections WHERE workspace_id = $1
	`, sandboxID).Scan(&remaining); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("a deleted sandbox left %d connection rows behind", remaining)
	}
}
