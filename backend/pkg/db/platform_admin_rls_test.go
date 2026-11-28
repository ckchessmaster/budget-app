package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPlatformAdminMetadataAndPrivacyRLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}
	defer adminPool.Close()

	appPool, err := pgxpool.New(ctx, getTestDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect to app database: %v", err)
	}
	defer appPool.Close()

	u1 := uuid.New()
	u2 := uuid.New()
	w1 := uuid.New()
	w1Demo := uuid.New()
	w2 := uuid.New()
	a1 := uuid.New()
	c1 := uuid.New()

	email1 := "admin-rls-u1-" + u1.String()[:8] + "@example.com"
	email2 := "admin-rls-u2-" + u2.String()[:8] + "@example.com"

	_, err = adminPool.Exec(ctx, `
		INSERT INTO platform_users (user_id, email, display_name, is_active)
		VALUES ($1, $2, 'User One', TRUE), ($3, $4, 'User Two', TRUE)
	`, u1, email1, u2, email2)
	if err != nil {
		t.Fatalf("failed to seed platform_users: %v", err)
	}

	_, err = adminPool.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo, owner_user_id)
		VALUES ($1, 'Personal One', FALSE, $2),
		       ($3, 'Demo Sandbox', TRUE, $2),
		       ($4, 'Personal Two', FALSE, $5)
	`, w1, u1, w1Demo, w2, u2)
	if err != nil {
		t.Fatalf("failed to seed workspaces: %v", err)
	}

	_, err = adminPool.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner'),
		       ($3, $2, 'owner'),
		       ($4, $5, 'owner'),
		       ($4, $2, 'member')
	`, w1, u1, w1Demo, w2, u2)
	if err != nil {
		t.Fatalf("failed to seed workspace_members: %v", err)
	}

	_, err = adminPool.Exec(ctx, `
		INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget, rollover_mode)
		VALUES ($1, $2, 'Secret Groceries', 'Essentials', 500.00, 'SWEEP')
	`, c1, w1)
	if err != nil {
		t.Fatalf("failed to seed category: %v", err)
	}

	_, err = adminPool.Exec(ctx, `
		INSERT INTO accounts (id, workspace_id, name, type, current_balance)
		VALUES ($1, $2, 'Secret Checking', 'checking', 1234.56)
	`, a1, w1)
	if err != nil {
		t.Fatalf("failed to seed account: %v", err)
	}

	_, err = adminPool.Exec(ctx, `
		INSERT INTO transactions (workspace_id, account_id, category_id, amount, merchant_raw, transaction_date)
		VALUES ($1, $2, $3, -42.00, 'Private Coffee', '2026-11-28')
	`, w1, a1, c1)
	if err != nil {
		t.Fatalf("failed to seed transaction: %v", err)
	}

	defer func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE id IN ($1, $2, $3)", w1, w1Demo, w2)
		_, _ = adminPool.Exec(bg, "DELETE FROM platform_users WHERE user_id IN ($1, $2)", u1, u2)
	}()

	// 1. Without platform_admin_mode GUC, overview returns 0 rows and admin functions fail
	var noModeCount int
	if err := appPool.QueryRow(ctx, "SELECT COUNT(*) FROM admin_workspace_overview WHERE workspace_id IN ($1, $2, $3)", w1, w1Demo, w2).Scan(&noModeCount); err != nil {
		t.Fatalf("unexpected error querying admin_workspace_overview without mode: %v", err)
	}
	if noModeCount != 0 {
		t.Fatalf("expected 0 overview rows without platform_admin_mode, got %d", noModeCount)
	}
	if _, err := appPool.Exec(ctx, "SELECT * FROM admin_list_users()"); err == nil {
		t.Fatal("expected admin_list_users() to reject calls without platform_admin_mode")
	}

	// 2. With WithPlatformAdminTx, overview shows non-demo workspaces only, while ledger tables remain isolated (0 rows)
	err = db.WithPlatformAdminTx(ctx, appPool, func(tx pgx.Tx) error {
		var overviewCount int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM admin_workspace_overview WHERE workspace_id IN ($1, $2, $3)", w1, w1Demo, w2).Scan(&overviewCount); err != nil {
			return err
		}
		if overviewCount != 2 {
			t.Fatalf("expected 2 non-demo overview rows, got %d", overviewCount)
		}

		for _, tbl := range []string{"transactions", "accounts", "categories", "receipts"} {
			var cnt int
			if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM "+tbl+" WHERE workspace_id = $1", w1).Scan(&cnt); err != nil {
				return err
			}
			if cnt != 0 {
				t.Fatalf("privacy violation: %s returned %d rows in platform admin tx", tbl, cnt)
			}
		}

		var owned, member int
		if err := tx.QueryRow(ctx, "SELECT owned_workspace_count, member_workspace_count FROM admin_list_users() WHERE user_id = $1", u1).Scan(&owned, &member); err != nil {
			return err
		}
		if owned != 1 || member != 1 {
			t.Fatalf("expected owned=1, member=1 for u1, got owned=%d, member=%d", owned, member)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithPlatformAdminTx query failed: %v", err)
	}

	// 3. admin_delete_user deletes u1's owned workspaces (w1, w1Demo), removes u1 from w2, and preserves w2
	var deleted bool
	err = db.WithPlatformAdminTx(ctx, appPool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT admin_delete_user($1)", u1).Scan(&deleted)
	})
	if err != nil {
		t.Fatalf("admin_delete_user failed: %v", err)
	}
	if !deleted {
		t.Fatal("expected admin_delete_user to return true")
	}

	var w1Exists, w2Exists bool
	_ = adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = $1)", w1).Scan(&w1Exists)
	_ = adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = $1)", w2).Scan(&w2Exists)
	if w1Exists {
		t.Fatal("expected sole-owned workspace w1 to be cascade deleted")
	}
	if !w2Exists {
		t.Fatal("expected shared workspace w2 owned by u2 to remain intact")
	}
}

func TestPlatformAdminNonSuperuserOwnerVisibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}
	defer adminPool.Close()

	// Create a non-superuser role (no SUPERUSER, no BYPASSRLS) to simulate production DB ownership
	roleName := "budget_nonsuper_test"
	if _, err := adminPool.Exec(ctx, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+roleName+`') THEN
				CREATE ROLE `+roleName+` WITH LOGIN PASSWORD 'nonsuperpass';
			END IF;
		END
		$$;
		GRANT USAGE, CREATE ON SCHEMA public TO `+roleName+`;
		GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO `+roleName+`;
		GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO `+roleName+`;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO `+roleName+`;
	`); err != nil {
		t.Fatalf("create non-superuser role: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	cfg.ConnConfig.User = roleName
	cfg.ConnConfig.Password = "nonsuperpass"
	nonSuperPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect as non-superuser: %v", err)
	}
	defer nonSuperPool.Close()

	u1 := uuid.New()
	w1 := uuid.New()
	email1 := "nonsuper-" + u1.String()[:8] + "@example.com"

	_, err = adminPool.Exec(ctx, `INSERT INTO platform_users (user_id, email, display_name, is_active) VALUES ($1, $2, 'NonSuper User', TRUE)`, u1, email1)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = adminPool.Exec(ctx, `INSERT INTO workspaces (id, name, is_demo, owner_user_id) VALUES ($1, 'NonSuper WS', FALSE, $2)`, w1, u1)
	if err != nil {
		t.Fatalf("seed ws: %v", err)
	}
	_, err = adminPool.Exec(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, w1, u1)
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}
	_, err = adminPool.Exec(ctx, `INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, last_synced_at, last_sync_status) VALUES ($1, 'enc', NOW(), 'OK')`, w1)
	if err != nil {
		t.Fatalf("seed sf: %v", err)
	}

	defer func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE id = $1", w1)
		_, _ = adminPool.Exec(bg, "DELETE FROM platform_users WHERE user_id = $1", u1)
	}()

	err = db.WithPlatformAdminTx(ctx, nonSuperPool, func(tx pgx.Tx) error {
		var wsName, ownerEmail, sfStatus string
		var memberCount int
		if err := tx.QueryRow(ctx, `
			SELECT workspace_name, COALESCE(owner_email, ''), member_count, simplefin_status
			FROM admin_workspace_overview WHERE workspace_id = $1
		`, w1).Scan(&wsName, &ownerEmail, &memberCount, &sfStatus); err != nil {
			return err
		}
		if wsName != "NonSuper WS" || ownerEmail != email1 || memberCount != 1 || sfStatus != "OK" {
			t.Fatalf("unexpected overview row: name=%q owner=%q members=%d sf=%q", wsName, ownerEmail, memberCount, sfStatus)
		}

		var owned int
		if err := tx.QueryRow(ctx, "SELECT owned_workspace_count FROM admin_list_users() WHERE user_id = $1", u1).Scan(&owned); err != nil {
			return err
		}
		if owned != 1 {
			t.Fatalf("expected owned_workspace_count=1 for non-superuser caller, got %d", owned)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("non-superuser WithPlatformAdminTx failed: %v", err)
	}
}
