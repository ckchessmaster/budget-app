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

	t.Run("accepts cash account type", func(t *testing.T) {
		a := &Account{
			Name:           "Household Cash",
			Type:           Cash,
			CurrentBalance: decimal.NewFromFloat(120.00),
			IsOnBudget:     true,
		}
		if err := ValidateAccount(a); err != nil {
			t.Fatalf("unexpected error for cash account: %v", err)
		}
	})
}

func TestApplyAccountUpdate(t *testing.T) {
	t.Run("allows updating numeric fields to zero and clearing nullable fields", func(t *testing.T) {
		due := 15
		catID := uuid.New()
		acct := &Account{
			ID:                   uuid.New(),
			Name:                 "Revolver Card",
			Type:                 CreditCard,
			CurrentBalance:       decimal.NewFromFloat(-500.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			InitialDebtBaseline:  decimal.NewFromFloat(1000.00),
			APRPercentage:        decimal.NewFromFloat(22.99),
			StatementDueDay:      &due,
			DebtPayoffCategoryID: &catID,
		}

		zero := decimal.Zero
		payInFull := PayInFull
		delta, err := applyAccountUpdate(acct, UpdateAccountRequest{
			CurrentBalance:          &zero,
			APRPercentage:           &zero,
			InitialDebtBaseline:     &zero,
			CardMode:                &payInFull,
			ClearStatementDueDay:    true,
			ClearDebtPayoffCategory: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if delta.StringFixed(2) != "500.00" {
			t.Fatalf("delta = %s, want 500.00", delta.StringFixed(2))
		}
		if !acct.CurrentBalance.IsZero() || !acct.APRPercentage.IsZero() || !acct.InitialDebtBaseline.IsZero() {
			t.Fatalf("expected zeroed numeric fields, got bal=%s apr=%s base=%s",
				acct.CurrentBalance, acct.APRPercentage, acct.InitialDebtBaseline)
		}
		if acct.StatementDueDay != nil || acct.DebtPayoffCategoryID != nil {
			t.Fatalf("expected cleared nullable fields, got due=%v cat=%v", acct.StatementDueDay, acct.DebtPayoffCategoryID)
		}
	})

	t.Run("switching from DEBT_PAYDOWN credit card to checking cleans up card fields", func(t *testing.T) {
		due := 20
		catID := uuid.New()
		acct := &Account{
			ID:                   uuid.New(),
			Name:                 "Card",
			Type:                 CreditCard,
			CurrentBalance:       decimal.NewFromFloat(-250.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			InitialDebtBaseline:  decimal.NewFromFloat(300.00),
			StatementDueDay:      &due,
			DebtPayoffCategoryID: &catID,
		}

		checking := Checking
		newBal := decimal.NewFromFloat(100.00)
		delta, err := applyAccountUpdate(acct, UpdateAccountRequest{
			Type:           &checking,
			CurrentBalance: &newBal,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if delta.StringFixed(2) != "350.00" {
			t.Fatalf("delta = %s, want 350.00", delta.StringFixed(2))
		}
		if acct.CardMode != PayInFull || acct.DebtPayoffCategoryID != nil || acct.StatementDueDay != nil || !acct.InitialDebtBaseline.IsZero() {
			t.Fatalf("expected card fields cleaned up, got %+v", acct)
		}
	})

	t.Run("rejects balance edit on SimpleFIN-linked account unless unlinked", func(t *testing.T) {
		extID := "sf-acct-1"
		acct := &Account{
			ID:                uuid.New(),
			ExternalAccountID: &extID,
			Name:              "Bank Checking",
			Type:              Checking,
			CurrentBalance:    decimal.NewFromFloat(1000.00),
			IsOnBudget:        true,
		}

		newBal := decimal.NewFromFloat(850.00)
		if _, err := applyAccountUpdate(acct, UpdateAccountRequest{CurrentBalance: &newBal}); !errors.Is(err, ErrLinkedAccountBalanceReadOnly) {
			t.Fatalf("expected ErrLinkedAccountBalanceReadOnly, got %v", err)
		}

		// Metadata-only update with unchanged balance succeeds
		renamed := "Renamed Bank Checking"
		sameBal := decimal.NewFromFloat(1000.00)
		delta, err := applyAccountUpdate(acct, UpdateAccountRequest{
			Name:           &renamed,
			CurrentBalance: &sameBal,
		})
		if err != nil || !delta.IsZero() || acct.Name != renamed {
			t.Fatalf("expected metadata update to succeed, got delta=%s err=%v", delta, err)
		}

		// Unlink and modify balance in same patch succeeds and returns negative delta
		delta, err = applyAccountUpdate(acct, UpdateAccountRequest{
			UnlinkExternal: true,
			CurrentBalance: &newBal,
		})
		if err != nil {
			t.Fatalf("expected unlink + balance update to succeed, got %v", err)
		}
		if acct.ExternalAccountID != nil {
			t.Fatalf("expected ExternalAccountID to be nil after unlink")
		}
		if delta.StringFixed(2) != "-150.00" {
			t.Fatalf("delta = %s, want -150.00", delta.StringFixed(2))
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
