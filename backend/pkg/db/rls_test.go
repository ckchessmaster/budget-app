package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestDatabaseURL() string {
	url := os.Getenv("TEST_DATABASE_URL")
	if url != "" {
		return url
	}
	url = os.Getenv("DATABASE_URL")
	if url != "" {
		return url
	}
	// Connect as unprivileged budget_app_user to enforce RLS
	return "postgres://budget_app_user:budgetpass@localhost:5432/budgetdb?sslmode=disable"
}

func getAdminDatabaseURL() string {
	url := os.Getenv("DATABASE_URL")
	if url != "" {
		return url
	}
	return "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
}

func TestRLSIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}
	defer adminPool.Close()

	if os.Getenv("TEST_DATABASE_URL") == "" && os.Getenv("DATABASE_URL") == "" {
		_, err = adminPool.Exec(ctx, `
			DO $$
			BEGIN
				IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'budget_app_user') THEN
					CREATE ROLE budget_app_user WITH LOGIN PASSWORD 'budgetpass';
				END IF;
			END
			$$;
			GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO budget_app_user;
			GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO budget_app_user;
			GRANT ALL PRIVILEGES ON SCHEMA public TO budget_app_user;
		`)
		if err != nil {
			t.Fatalf("failed to ensure test role budget_app_user: %v", err)
		}
	}

	appPool, err := pgxpool.New(ctx, getTestDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect to app database: %v", err)
	}
	defer appPool.Close()

	// 1. Create Workspace W1 with User U1; Create Workspace W2 with User U2.
	u1 := uuid.New()
	u2 := uuid.New()
	w1 := uuid.New()
	w2 := uuid.New()

	// Admin setup bypassing RLS
	if _, err = adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Workspace 1'), ($2, 'Workspace 2')", w1, w2); err != nil {
		t.Fatalf("failed to setup workspaces: %v", err)
	}
	if _, err = adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner'), ($3, $4, 'owner')", w1, u1, w2, u2); err != nil {
		t.Fatalf("failed to setup members: %v", err)
	}

	defer func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM push_subscriptions WHERE user_id IN ($1, $2)", u1, u2)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id IN ($1, $2)", w1, w2)
	}()

	// 2. Insert Category C1 and Income I1 into W1; insert C2 and I2 into W2.
	c1 := uuid.New()
	c2 := uuid.New()
	i1 := uuid.New()
	i2 := uuid.New()
	month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	_, err = adminPool.Exec(ctx, `
		INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget, rollover_mode)
		VALUES ($1, $2, 'Category C1', 'Essentials', 100.00, 'SWEEP'),
		       ($3, $4, 'Category C2', 'Lifestyle', 200.00, 'SWEEP')
	`, c1, w1, c2, w2)
	if err != nil {
		t.Fatalf("failed to insert test categories: %v", err)
	}

	_, err = adminPool.Exec(ctx, `
		INSERT INTO monthly_planned_incomes (id, workspace_id, year_month, name, amount)
		VALUES ($1, $2, $5, 'Income I1', 3000.00),
		       ($3, $4, $5, 'Income I2', 4000.00)
	`, i1, w1, i2, w2, month)
	if err != nil {
		t.Fatalf("failed to insert test incomes: %v", err)
	}

	nextMonth := month.AddDate(0, 1, 0)
	r1, r2 := uuid.New(), uuid.New()
	_, err = adminPool.Exec(ctx, `
		INSERT INTO month_end_rollover_runs (id, workspace_id, source_month, target_month, total_rolled_over)
		VALUES ($1, $2, $5, $6, 100.00),
		       ($3, $4, $5, $6, 200.00)
	`, r1, w1, r2, w2, month, nextMonth)
	if err != nil {
		t.Fatalf("failed to insert test rollover runs: %v", err)
	}

	il1, il2 := uuid.New(), uuid.New()
	_, err = adminPool.Exec(ctx, `
		INSERT INTO credit_card_debt_interlocks (id, workspace_id, source_month, target_month, deficit_amount)
		VALUES ($1, $2, $5, $6, 45.00),
		       ($3, $4, $5, $6, 90.00)
	`, il1, w1, il2, w2, month, nextMonth)
	if err != nil {
		t.Fatalf("failed to insert test cc debt interlocks: %v", err)
	}

	adj1, adj2 := uuid.New(), uuid.New()
	_, err = adminPool.Exec(ctx, `
		INSERT INTO monthly_rollover_adjustments (id, workspace_id, source_month, target_month, adjustment_type, amount)
		VALUES ($1, $2, $5, $6, 'SWEEP_TO_POOL', 25.00),
		       ($3, $4, $5, $6, 'SWEEP_TO_POOL', 75.00)
	`, adj1, w1, adj2, w2, month, nextMonth)
	if err != nil {
		t.Fatalf("failed to insert test rollover adjustments: %v", err)
	}

	ps1, ps2 := uuid.New(), uuid.New()
	_, err = adminPool.Exec(ctx, `
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth)
		VALUES ($1, $2, $5, 'p256dh-1', 'auth-1'),
		       ($3, $4, $6, 'p256dh-2', 'auth-2')
	`, ps1, u1, ps2, u2, "https://push.example.test/"+ps1.String(), "https://push.example.test/"+ps2.String())
	if err != nil {
		t.Fatalf("failed to insert test push subscriptions: %v", err)
	}

	nc1, nc2 := uuid.New(), uuid.New()
	_, err = adminPool.Exec(ctx, `
		INSERT INTO notification_cooldowns (id, workspace_id, trigger_type, entity_id)
		VALUES ($1, $2, 'SPENDING_VELOCITY', $5),
		       ($3, $4, 'STATEMENT_DUE', $6)
	`, nc1, w1, nc2, w2, c1, c2)
	if err != nil {
		t.Fatalf("failed to insert test notification cooldowns: %v", err)
	}

	// 3. Open connection as budget_app_user, execute SET LOCAL app.current_user_id = '<U1_UUID>'
	conn, err := appPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire app connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)", u1.String())
	if err != nil {
		t.Fatalf("failed to set app.current_user_id: %v", err)
	}

	// 4. Assert: SELECT * FROM categories; returns only C1.
	catRows, err := tx.Query(ctx, "SELECT id, name FROM categories")
	if err != nil {
		t.Fatalf("failed to query categories: %v", err)
	}
	defer catRows.Close()

	var returnedCats []uuid.UUID
	for catRows.Next() {
		var catID uuid.UUID
		var catName string
		if err := catRows.Scan(&catID, &catName); err != nil {
			t.Fatalf("scan category failed: %v", err)
		}
		returnedCats = append(returnedCats, catID)
	}

	if len(returnedCats) != 1 || returnedCats[0] != c1 {
		t.Fatalf("expected only category C1 (%s), got: %v", c1, returnedCats)
	}

	// 5. Assert: SELECT * FROM monthly_planned_incomes; returns only I1.
	incRows, err := tx.Query(ctx, "SELECT id, name FROM monthly_planned_incomes")
	if err != nil {
		t.Fatalf("failed to query incomes: %v", err)
	}
	defer incRows.Close()

	var returnedIncomes []uuid.UUID
	for incRows.Next() {
		var incID uuid.UUID
		var incName string
		if err := incRows.Scan(&incID, &incName); err != nil {
			t.Fatalf("scan income failed: %v", err)
		}
		returnedIncomes = append(returnedIncomes, incID)
	}

	if len(returnedIncomes) != 1 || returnedIncomes[0] != i1 {
		t.Fatalf("expected only income I1 (%s), got: %v", i1, returnedIncomes)
	}

	// 6. Assert: new rollover tables only return W1 rows.
	assertOnlyID := func(table string, wantID uuid.UUID) {
		rows, err := tx.Query(ctx, "SELECT id FROM "+table)
		if err != nil {
			t.Fatalf("failed to query %s: %v", table, err)
		}
		defer rows.Close()
		var got []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan %s failed: %v", table, err)
			}
			got = append(got, id)
		}
		if len(got) != 1 || got[0] != wantID {
			t.Fatalf("expected only %s (%s) in %s, got: %v", wantID, wantID, table, got)
		}
	}
	assertOnlyID("month_end_rollover_runs", r1)
	assertOnlyID("credit_card_debt_interlocks", il1)
	assertOnlyID("monthly_rollover_adjustments", adj1)
	assertOnlyID("push_subscriptions", ps1)
	assertOnlyID("notification_cooldowns", nc1)
}

func TestRLSDemoWorkspaceIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	realWS := uuid.New()
	demoWS := uuid.New()

	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name, is_demo) VALUES ($1, 'Real WS', FALSE), ($2, 'Demo WS', TRUE)", realWS, demoWS); err != nil {
		t.Fatalf("failed to setup workspaces: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $3, 'owner'), ($2, $3, 'owner')", realWS, demoWS, u1); err != nil {
		t.Fatalf("failed to setup memberships: %v", err)
	}
	defer func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM user_preferences WHERE user_id = $1", u1)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id IN ($1, $2)", realWS, demoWS)
	}()

	realCat := uuid.New()
	demoCat := uuid.New()
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget, rollover_mode)
		VALUES ($1, $2, 'Real Cat', 'Essentials', 100.00, 'SWEEP'),
		       ($3, $4, 'Demo Cat', 'Essentials', 200.00, 'SWEEP')
	`, realCat, realWS, demoCat, demoWS); err != nil {
		t.Fatalf("failed to insert categories: %v", err)
	}

	// Demo mode disabled -> sees only Real WS
	err = db.WithWorkspaceTx(ctx, appPool, u1, func(tx pgx.Tx) error {
		activeID, err := db.ActiveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		if activeID != realWS {
			t.Fatalf("ActiveWorkspaceID = %s, want realWS %s", activeID, realWS)
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE id = $1", demoCat).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("expected demo category hidden when demo mode off, got count=%d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("real mode check failed: %v", err)
	}

	// Enable demo mode -> sees only Demo WS
	if _, err := adminPool.Exec(ctx, "INSERT INTO user_preferences (user_id, demo_mode_enabled) VALUES ($1, TRUE)", u1); err != nil {
		t.Fatalf("failed to enable demo mode preference: %v", err)
	}

	err = db.WithWorkspaceTx(ctx, appPool, u1, func(tx pgx.Tx) error {
		activeID, err := db.ActiveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		if activeID != demoWS {
			t.Fatalf("ActiveWorkspaceID = %s, want demoWS %s", activeID, demoWS)
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE id = $1", realCat).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("expected real category hidden when demo mode on, got count=%d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("demo mode check failed: %v", err)
	}
}
