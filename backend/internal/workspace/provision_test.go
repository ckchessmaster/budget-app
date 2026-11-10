package workspace_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
)

func getAdminDatabaseURL() string {
	url := os.Getenv("DATABASE_URL")
	if url != "" {
		return url
	}
	return "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
}

func TestEnsureWorkspaceProvisioned(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	defer pool.Close()

	provisioner := workspace.NewProvisioner()
	testUserID := uuid.New()

	// Test Case 1: Fresh user calls EnsureWorkspaceProvisioned
	result1, err := provisioner.EnsureWorkspaceProvisioned(ctx, pool, testUserID)
	if err != nil {
		t.Fatalf("EnsureWorkspaceProvisioned failed on first call: %v", err)
	}
	if !result1.IsNewUser {
		t.Errorf("expected IsNewUser == true for fresh user, got false")
	}
	if result1.WorkspaceID == uuid.Nil {
		t.Errorf("expected non-nil WorkspaceID")
	}
	if result1.InboundReceiptMail == "" {
		t.Errorf("expected non-empty InboundReceiptMail")
	}

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", result1.WorkspaceID)
	}()

	// Assert workspace exists
	var wsCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM workspaces WHERE id = $1", result1.WorkspaceID).Scan(&wsCount)
	if err != nil || wsCount != 1 {
		t.Fatalf("expected workspace to exist, found %d", wsCount)
	}

	// Assert 10 default categories seeded
	var catCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE workspace_id = $1", result1.WorkspaceID).Scan(&catCount)
	if err != nil {
		t.Fatalf("failed to count categories: %v", err)
	}
	if catCount != 10 {
		t.Fatalf("expected 10 default categories seeded, got %d", catCount)
	}

	// Test Case 2: Same user calls EnsureWorkspaceProvisioned again
	result2, err := provisioner.EnsureWorkspaceProvisioned(ctx, pool, testUserID)
	if err != nil {
		t.Fatalf("EnsureWorkspaceProvisioned failed on second call: %v", err)
	}
	if result2.IsNewUser {
		t.Errorf("expected IsNewUser == false for existing user, got true")
	}
	if result2.WorkspaceID != result1.WorkspaceID {
		t.Errorf("expected same WorkspaceID (%s), got (%s)", result1.WorkspaceID, result2.WorkspaceID)
	}
	if result2.InboundReceiptMail != result1.InboundReceiptMail {
		t.Errorf("expected same InboundReceiptMail (%s), got (%s)", result1.InboundReceiptMail, result2.InboundReceiptMail)
	}

	// Assert no duplicate categories created
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM categories WHERE workspace_id = $1", result1.WorkspaceID).Scan(&catCount)
	if err != nil || catCount != 10 {
		t.Fatalf("expected category count to remain 10, got %d", catCount)
	}
}
