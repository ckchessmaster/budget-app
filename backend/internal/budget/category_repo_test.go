package budget

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func TestSanitizeCategory(t *testing.T) {
	id := uuid.New()
	otherID := uuid.New()

	t.Run("rejects cyclic sweep reference", func(t *testing.T) {
		c := &Category{
			ID:                    id,
			RolloverMode:          RolloverSweep,
			SweepTargetCategoryID: &id,
		}
		if err := sanitizeCategory(c); err != ErrCyclicSweep {
			t.Fatalf("expected ErrCyclicSweep, got %v", err)
		}
	})

	t.Run("clears sweep target when rollover mode is not SWEEP", func(t *testing.T) {
		c := &Category{
			ID:                    id,
			RolloverMode:          RolloverBalance,
			SweepTargetCategoryID: &otherID,
		}
		if err := sanitizeCategory(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.SweepTargetCategoryID != nil {
			t.Fatalf("expected SweepTargetCategoryID to be nil, got %v", c.SweepTargetCategoryID)
		}
	})
}

func openTestDB(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		t.Skipf("skipping DB integration test (admin pool error): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping DB integration test (postgres unreachable): %v", err)
	}

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping DB integration test (app pool error): %v", err)
	}

	wsID := uuid.New()
	userID := uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Test Workspace')", wsID); err != nil {
		adminPool.Close()
		appPool.Close()
		t.Fatalf("failed to create test workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		adminPool.Close()
		appPool.Close()
		t.Fatalf("failed to create test member: %v", err)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
		adminPool.Close()
		appPool.Close()
	})

	return adminPool, appPool, wsID, userID
}

func TestCategoryRepoCRUD(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	repo := NewCategoryRepo()
	ctx := context.Background()

	var createdID uuid.UUID
	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		cat := &Category{
			WorkspaceID:   wsID,
			Name:          "Groceries",
			GroupName:     "Food",
			MonthlyBudget: decimal.NewFromFloat(500.00),
			RolloverMode:  RolloverSweep,
			TargetCadence: "NONE",
			SortOrder:     1,
		}
		if err := repo.Create(ctx, tx, cat); err != nil {
			return err
		}
		createdID = cat.ID

		fetched, err := repo.GetByID(ctx, tx, createdID)
		if err != nil {
			return err
		}
		if fetched.Name != "Groceries" || !fetched.MonthlyBudget.Equal(decimal.NewFromFloat(500.00)) {
			t.Fatalf("unexpected fetched category: %+v", fetched)
		}

		fetched.Name = "Organic Groceries"
		fetched.MonthlyBudget = decimal.NewFromFloat(650.00)
		if err := repo.Update(ctx, tx, fetched); err != nil {
			return err
		}

		list, err := repo.ListByWorkspace(ctx, tx)
		if err != nil {
			return err
		}
		if len(list) != 1 || list[0].Name != "Organic Groceries" {
			t.Fatalf("unexpected list result: %+v", list)
		}

		if err := repo.Delete(ctx, tx, createdID); err != nil {
			return err
		}

		if _, err := repo.GetByID(ctx, tx, createdID); err != ErrNotFound {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestCategoryRepoCRUD failed: %v", err)
	}
}
