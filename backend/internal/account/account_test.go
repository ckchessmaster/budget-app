package account

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestAccountService(t *testing.T) {
	svc := NewAccountService(nil)
	ctx := context.Background()

	t.Run("rejects DEBT_PAYDOWN without debt_payoff_category_id", func(t *testing.T) {
		err := svc.CreateAccount(ctx, nil, &Account{
			Name:           "Chase Sapphire",
			Type:           CreditCard,
			CurrentBalance: decimal.NewFromFloat(-1200.00),
			IsOnBudget:     true,
			CardMode:       DebtPaydown,
		})
		if !errors.Is(err, ErrMissingDebtCategory) {
			t.Fatalf("expected ErrMissingDebtCategory, got %v", err)
		}
	})

	t.Run("rejects DEBT_PAYDOWN on non-credit-card account", func(t *testing.T) {
		catID := uuid.New()
		err := svc.CreateAccount(ctx, nil, &Account{
			Name:                 "Checking",
			Type:                 Checking,
			CurrentBalance:       decimal.NewFromFloat(1000.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			DebtPayoffCategoryID: &catID,
		})
		if !errors.Is(err, ErrInvalidCardMode) {
			t.Fatalf("expected ErrInvalidCardMode, got %v", err)
		}
	})

	t.Run("accepts valid DEBT_PAYDOWN credit card", func(t *testing.T) {
		catID := uuid.New()
		err := ValidateAccount(&Account{
			Name:                 "Chase Freedom",
			Type:                 CreditCard,
			CurrentBalance:       decimal.NewFromFloat(-2500.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			DebtPayoffCategoryID: &catID,
		})
		if err != nil {
			t.Fatalf("expected valid account, got %v", err)
		}
	})
}
