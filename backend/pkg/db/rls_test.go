package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
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
}
