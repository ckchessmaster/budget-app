package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

func TestWithWorkspaceTx(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	userID := uuid.New()
	workspaceID := uuid.New()

	// Setup workspace and member using admin pool
	if _, err = adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Tx Test Workspace')", workspaceID); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	if _, err = adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", workspaceID, userID); err != nil {
		t.Fatalf("failed to create test workspace member: %v", err)
	}
	defer func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", workspaceID)
	}()

	categoryID := uuid.New()

	// 1. Rollback on returned error
	expectedErr := errors.New("simulated error to trigger rollback")
	err = db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `
			INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget, rollover_mode)
			VALUES ($1, $2, 'Temporary Category', 'Test', 50.00, 'SWEEP')
		`, categoryID, workspaceID)
		if execErr != nil {
			return execErr
		}
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	// Verify category does not exist in database
	var count int
	err = adminPool.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE id = $1", categoryID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to verify category absence: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected category to be rolled back, but found in database")
	}

	// 2. Commit on success
	commitCategoryID := uuid.New()
	err = db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `
			INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget, rollover_mode)
			VALUES ($1, $2, 'Committed Category', 'Test', 75.00, 'SWEEP')
		`, commitCategoryID, workspaceID)
		return execErr
	})
	if err != nil {
		t.Fatalf("expected commit to succeed, got %v", err)
	}

	err = adminPool.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE id = $1", commitCategoryID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query committed category: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 category record committed, got %d", count)
	}
}

func TestWithWorkspaceTx_RollbackOnPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	userID := uuid.New()
	workspaceID := uuid.New()

	if _, err = adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Panic Test Workspace')", workspaceID); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}
	if _, err = adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", workspaceID, userID); err != nil {
		t.Fatalf("failed to create test member: %v", err)
	}
	defer func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", workspaceID)
	}()

	panicCategoryID := uuid.New()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic to propagate")
		}

		var count int
		_ = adminPool.QueryRow(context.Background(), "SELECT COUNT(*) FROM categories WHERE id = $1", panicCategoryID).Scan(&count)
		if count != 0 {
			t.Fatalf("expected category to be rolled back on panic, but found %d records", count)
		}
	}()

	_ = db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `
			INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget, rollover_mode)
			VALUES ($1, $2, 'Panic Category', 'Test', 25.00, 'SWEEP')
		`, panicCategoryID, workspaceID)
		if execErr != nil {
			return execErr
		}
		panic("panic inside tx")
	})
}
