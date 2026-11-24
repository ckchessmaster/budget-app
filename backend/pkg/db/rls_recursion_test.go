package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

// rlsProtectedTables mirrors every table that migration 000002 enabled and forced
// row-level security on, plus the tables added later.
var rlsProtectedTables = []string{
	"workspaces",
	"workspace_members",
	"workspace_settings",
	"accounts",
	"categories",
	"monthly_category_budgets",
	"monthly_planned_incomes",
	"budget_reallocations",
	"transactions",
	"receipts",
	"simplefin_connections",
	"user_preferences",
	"demo_simplefin_state",
	"demo_simplefin_accounts",
	"demo_simplefin_transactions",
	"month_end_rollover_runs",
	"credit_card_debt_interlocks",
	"monthly_rollover_adjustments",
}

// TestRLSPoliciesTerminateWithoutRecursion guards the row-security policy chain: scanning
// any protected table must resolve the caller's workspaces without re-entering a policy
// expression that reads that same table.
//
// It reproduces the production role topology, where the role that runs migrations also owns
// the SECURITY DEFINER helpers, so those helpers stay subject to FORCE ROW LEVEL SECURITY.
// CI had been migrating as a superuser, which bypasses row security inside the helpers: the
// workspace_isolation -> current_app_user_workspace_ids() cycle added by migration 000005
// therefore passed CI while every deployed API read died with "stack depth limit exceeded".
func TestRLSPoliciesTerminateWithoutRecursion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect admin: %v", err)
	}
	defer adminPool.Close()

	appPool, err := pgxpool.New(ctx, getTestDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect app: %v", err)
	}
	defer appPool.Close()

	var appRole string
	var appIsSuperuser bool
	if err := appPool.QueryRow(ctx, `
		SELECT current_user, (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)
	`).Scan(&appRole, &appIsSuperuser); err != nil {
		t.Fatalf("failed to read app role properties: %v", err)
	}
	if appIsSuperuser {
		t.Skip("app role is a superuser, which bypasses row security: recursion cannot be reproduced")
	}

	// Own every SECURITY DEFINER helper as the unprivileged app role, the way production
	// does, so row security stays in force inside them.
	type definerFunction struct {
		signature   string
		previousOwn string
	}
	rows, err := adminPool.Query(ctx, `
		SELECT p.oid::regprocedure::text, r.rolname
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE n.nspname = 'public' AND p.prosecdef
	`)
	if err != nil {
		t.Fatalf("failed to list security definer functions: %v", err)
	}
	var definers []definerFunction
	for rows.Next() {
		var d definerFunction
		if err := rows.Scan(&d.signature, &d.previousOwn); err != nil {
			rows.Close()
			t.Fatalf("failed to scan security definer function: %v", err)
		}
		definers = append(definers, d)
	}
	rows.Close()
	if len(definers) == 0 {
		t.Fatal("expected at least one SECURITY DEFINER helper function")
	}

	reowned := make([]definerFunction, 0, len(definers))
	for _, d := range definers {
		if d.previousOwn == appRole {
			continue
		}
		stmt := fmt.Sprintf("ALTER FUNCTION %s OWNER TO %s", d.signature, pgx.Identifier{appRole}.Sanitize())
		if _, err := adminPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("failed to re-own %s as %s: %v", d.signature, appRole, err)
		}
		reowned = append(reowned, d)
	}
	defer func() {
		for _, d := range reowned {
			stmt := fmt.Sprintf("ALTER FUNCTION %s OWNER TO %s", d.signature, pgx.Identifier{d.previousOwn}.Sanitize())
			if _, err := adminPool.Exec(context.Background(), stmt); err != nil {
				t.Errorf("failed to restore ownership of %s to %s: %v", d.signature, d.previousOwn, err)
			}
		}
	}()

	userID := uuid.New()
	workspaceID := uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Recursion Test WS')", workspaceID); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", workspaceID, userID); err != nil {
		t.Fatalf("failed to create test workspace member: %v", err)
	}
	defer func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", workspaceID)
	}()

	// A recursive policy chain aborts the statement, so each scan must be attempted in its
	// own transaction to report every table rather than only the first failure.
	for _, table := range rlsProtectedTables {
		table := table
		t.Run(table, func(t *testing.T) {
			err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
				activeID, err := db.ActiveWorkspaceID(ctx, tx)
				if err != nil {
					return fmt.Errorf("resolve active workspace: %w", err)
				}
				if activeID != workspaceID {
					return fmt.Errorf("active workspace = %s, want %s", activeID, workspaceID)
				}
				var count int
				if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
					return fmt.Errorf("scan %s: %w", table, err)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scanning %s as %s failed: %v", table, appRole, err)
			}
		})
	}
}
