package transaction

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

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

func TestValidateSplits(t *testing.T) {
	catA := uuid.New()
	catB := uuid.New()

	t.Run("rejects mismatched split sum by 1 cent", func(t *testing.T) {
		err := ValidateSplits(decimal.NewFromFloat(-100.00), []SplitItem{
			{Amount: decimal.NewFromFloat(-60.00), CategoryID: &catA},
			{Amount: decimal.NewFromFloat(-39.99), CategoryID: &catB},
		})
		if !errors.Is(err, ErrSplitSumMismatch) {
			t.Fatalf("expected ErrSplitSumMismatch, got %v", err)
		}
	})

	t.Run("accepts exact split sum", func(t *testing.T) {
		err := ValidateSplits(decimal.NewFromFloat(-100.00), []SplitItem{
			{Amount: decimal.NewFromFloat(-60.00), CategoryID: &catA},
			{Amount: decimal.NewFromFloat(-40.00), CategoryID: &catB},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts empty splits for unsplit transaction", func(t *testing.T) {
		if err := ValidateSplits(decimal.NewFromFloat(-25.50), nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSplitTransactionIntegration(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	acctRepo := account.NewRepo()
	svc := NewService(NewRepo(), acctRepo)
	ctx := context.Background()

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		acct := &account.Account{
			WorkspaceID:    wsID,
			Name:           "Checking",
			Type:           account.Checking,
			CurrentBalance: decimal.NewFromFloat(1000.00),
			IsOnBudget:     true,
		}
		if err := acctRepo.Create(ctx, tx, acct); err != nil {
			return err
		}

		cat1 := uuid.New()
		cat2 := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget)
			VALUES ($1, $3, 'Groceries', 'Food', 500.00),
			       ($2, $3, 'Household', 'Home', 200.00)
		`, cat1, cat2, wsID); err != nil {
			return err
		}

		// Mismatched split should be rejected
		_, err := svc.CreateTransaction(ctx, tx, CreateSplitTransactionRequest{
			AccountID:       acct.ID,
			TotalAmount:     decimal.NewFromFloat(-100.00),
			MerchantRaw:     "Target",
			TransactionDate: time.Now().UTC(),
			Splits: []SplitItem{
				{Amount: decimal.NewFromFloat(-60.00), CategoryID: &cat1},
				{Amount: decimal.NewFromFloat(-39.99), CategoryID: &cat2},
			},
		})
		if !errors.Is(err, ErrSplitSumMismatch) {
			t.Fatalf("expected ErrSplitSumMismatch, got %v", err)
		}

		// Valid split should succeed and adjust balance
		created, err := svc.CreateTransaction(ctx, tx, CreateSplitTransactionRequest{
			AccountID:       acct.ID,
			TotalAmount:     decimal.NewFromFloat(-100.00),
			MerchantRaw:     "Target",
			TransactionDate: time.Now().UTC(),
			Splits: []SplitItem{
				{Amount: decimal.NewFromFloat(-60.00), CategoryID: &cat1, UserNotes: "Food"},
				{Amount: decimal.NewFromFloat(-40.00), CategoryID: &cat2, UserNotes: "Supplies"},
			},
		})
		if err != nil {
			return err
		}
		if created.CategoryID != nil {
			t.Fatalf("expected parent CategoryID = nil, got %v", created.CategoryID)
		}
		if len(created.Splits) != 2 {
			t.Fatalf("expected 2 splits, got %d", len(created.Splits))
		}

		items, total, err := svc.ListTransactions(ctx, tx, TransactionFilter{AccountID: &acct.ID, Page: 1, Limit: 10})
		if err != nil {
			return err
		}
		if total != 1 || len(items) != 1 || len(items[0].Splits) != 2 {
			t.Fatalf("unexpected list result: total=%d items=%+v", total, items)
		}

		// Delete parent cascades to children and restores balance
		if err := svc.DeleteTransaction(ctx, tx, created.ID); err != nil {
			return err
		}
		updatedAcct, err := acctRepo.GetByID(ctx, tx, acct.ID)
		if err != nil {
			return err
		}
		if updatedAcct.CurrentBalance.StringFixed(2) != "1000.00" {
			t.Fatalf("balance after delete = %s, want 1000.00", updatedAcct.CurrentBalance.StringFixed(2))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("integration test failed: %v", err)
	}
}

type stubTxRepo struct {
	tx      *Transaction
	creates int
}

func (s *stubTxRepo) Create(_ context.Context, _ pgx.Tx, t *Transaction) error {
	s.creates++
	return nil
}
func (s *stubTxRepo) GetByID(_ context.Context, _ pgx.Tx, id uuid.UUID) (*Transaction, error) {
	if s.tx == nil || s.tx.ID != id {
		return nil, ErrTransactionNotFound
	}
	copyTx := *s.tx
	return &copyTx, nil
}
func (s *stubTxRepo) GetByClientID(_ context.Context, _ pgx.Tx, clientID string) (*Transaction, error) {
	if s.tx == nil || s.tx.ClientID == nil || *s.tx.ClientID != clientID {
		return nil, ErrTransactionNotFound
	}
	copyTx := *s.tx
	return &copyTx, nil
}
func (s *stubTxRepo) List(_ context.Context, _ pgx.Tx, _ TransactionFilter) ([]Transaction, int, error) {
	return nil, 0, nil
}
func (s *stubTxRepo) Update(_ context.Context, _ pgx.Tx, t *Transaction) error {
	copyTx := *t
	s.tx = &copyTx
	return nil
}
func (s *stubTxRepo) Delete(_ context.Context, _ pgx.Tx, _ uuid.UUID) error { return nil }

func TestUpdateTransaction_CategoryAndClearCategory(t *testing.T) {
	txID := uuid.New()
	catID := uuid.New()
	repo := &stubTxRepo{
		tx: &Transaction{
			ID:          txID,
			AccountID:   uuid.New(),
			Amount:      decimal.NewFromFloat(-42.15),
			MerchantRaw: "Target",
		},
	}
	svc := NewService(repo, nil)
	ctx := context.Background()

	updated, err := svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{
		CategoryID: &catID,
	})
	if err != nil {
		t.Fatalf("UpdateTransaction set category failed: %v", err)
	}
	if updated.CategoryID == nil || *updated.CategoryID != catID {
		t.Fatalf("expected CategoryID %v, got %v", catID, updated.CategoryID)
	}

	cleared, err := svc.UpdateTransaction(ctx, nil, txID, UpdateTransactionRequest{
		ClearCategory: true,
	})
	if err != nil {
		t.Fatalf("UpdateTransaction clear category failed: %v", err)
	}
	if cleared.CategoryID != nil {
		t.Fatalf("expected CategoryID to be nil after ClearCategory, got %v", cleared.CategoryID)
	}
}
