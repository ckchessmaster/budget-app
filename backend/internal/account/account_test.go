package account

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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

func TestAccountServiceValidation(t *testing.T) {
	catID := uuid.New()

	t.Run("rejects DEBT_PAYDOWN without debt_payoff_category_id", func(t *testing.T) {
		a := &Account{
			Name:           "Visa Revolver",
			Type:           CreditCard,
			CurrentBalance: decimal.NewFromFloat(-1200.00),
			IsOnBudget:     true,
			CardMode:       DebtPaydown,
		}
		err := ValidateAccount(a)
		if !errors.Is(err, ErrMissingDebtPayoffCat) {
			t.Fatalf("expected ErrMissingDebtPayoffCat, got %v", err)
		}
	})

	t.Run("rejects DEBT_PAYDOWN on non-credit card", func(t *testing.T) {
		a := &Account{
			Name:                 "Checking",
			Type:                 Checking,
			CurrentBalance:       decimal.NewFromFloat(500.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			DebtPayoffCategoryID: &catID,
		}
		err := ValidateAccount(a)
		if !errors.Is(err, ErrDebtPaydownNonCreditCard) {
			t.Fatalf("expected ErrDebtPaydownNonCreditCard, got %v", err)
		}
	})

	t.Run("accepts valid DEBT_PAYDOWN credit card", func(t *testing.T) {
		due := 15
		a := &Account{
			Name:                 "Chase Sapphire",
			Type:                 CreditCard,
			CurrentBalance:       decimal.NewFromFloat(-4500.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			APRPercentage:        decimal.NewFromFloat(24.00),
			StatementDueDay:      &due,
			DebtPayoffCategoryID: &catID,
		}
		if err := ValidateAccount(a); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects invalid statement_due_day", func(t *testing.T) {
		due := 32
		a := &Account{
			Name:            "Amex",
			Type:            CreditCard,
			CardMode:        PayInFull,
			StatementDueDay: &due,
		}
		if err := ValidateAccount(a); !errors.Is(err, ErrInvalidStatementDueDay) {
			t.Fatalf("expected ErrInvalidStatementDueDay, got %v", err)
		}
	})
}

func TestAccountServiceIntegration(t *testing.T) {
	_, appPool, _, userID := openTestDB(t)
	svc := NewService(NewRepo())
	ctx := context.Background()

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		badCard := &Account{
			Name:           "Bad Card",
			Type:           CreditCard,
			CurrentBalance: decimal.NewFromFloat(-500),
			IsOnBudget:     true,
			CardMode:       DebtPaydown,
		}
		if err := svc.CreateAccount(ctx, tx, badCard); !errors.Is(err, ErrMissingDebtPayoffCat) {
			t.Fatalf("expected ErrMissingDebtPayoffCat, got %v", err)
		}

		checking := &Account{
			Name:           "Primary Checking",
			Type:           Checking,
			CurrentBalance: decimal.NewFromFloat(2500.50),
			IsOnBudget:     true,
		}
		if err := svc.CreateAccount(ctx, tx, checking); err != nil {
			return err
		}

		got, err := svc.GetAccount(ctx, tx, checking.ID)
		if err != nil {
			return err
		}
		if got.Name != "Primary Checking" || got.CurrentBalance.StringFixed(2) != "2500.50" {
			t.Fatalf("unexpected account: %+v", got)
		}

		if err := svc.AdjustBalance(ctx, tx, checking.ID, decimal.NewFromFloat(-100.25)); err != nil {
			return err
		}
		got, err = svc.GetAccount(ctx, tx, checking.ID)
		if err != nil {
			return err
		}
		if got.CurrentBalance.StringFixed(2) != "2400.25" {
			t.Fatalf("balance after adjust = %s, want 2400.25", got.CurrentBalance.StringFixed(2))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
}
