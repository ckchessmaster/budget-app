package account

import (
	"context"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestCalculateRevolverSplit(t *testing.T) {
	catID := uuid.New()

	t.Run("splits 600 payment with 200 active spend into 200 transfer + 400 debt", func(t *testing.T) {
		res := CalculateRevolverSplit(
			decimal.NewFromFloat(200.00),
			decimal.NewFromFloat(600.00),
			catID,
		)
		if res.TransferLegAmount.StringFixed(2) != "200.00" {
			t.Fatalf("TransferLegAmount = %s, want 200.00", res.TransferLegAmount.StringFixed(2))
		}
		if res.DebtLegAmount.StringFixed(2) != "400.00" {
			t.Fatalf("DebtLegAmount = %s, want 400.00", res.DebtLegAmount.StringFixed(2))
		}
		if res.DebtCategoryID != catID {
			t.Fatalf("DebtCategoryID = %s, want %s", res.DebtCategoryID, catID)
		}
	})

	t.Run("caps transfer leg at payment amount when active spend exceeds payment", func(t *testing.T) {
		res := CalculateRevolverSplit(
			decimal.NewFromFloat(500.00),
			decimal.NewFromFloat(300.00),
			catID,
		)
		if res.TransferLegAmount.StringFixed(2) != "300.00" {
			t.Fatalf("TransferLegAmount = %s, want 300.00", res.TransferLegAmount.StringFixed(2))
		}
		if res.DebtLegAmount.StringFixed(2) != "0.00" {
			t.Fatalf("DebtLegAmount = %s, want 0.00", res.DebtLegAmount.StringFixed(2))
		}
	})
}

func TestProcessRevolverPayment(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		catID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO categories (id, workspace_id, name, group_name, monthly_budget)
			VALUES ($1, $2, 'Visa Paydown', 'Debt', 400.00)
		`, catID, wsID); err != nil {
			return err
		}

		card := Account{
			ID:                   uuid.New(),
			WorkspaceID:          wsID,
			Name:                 "Visa Revolver",
			Type:                 CreditCard,
			CurrentBalance:       decimal.NewFromFloat(-2200.00),
			IsOnBudget:           true,
			CardMode:             DebtPaydown,
			InitialDebtBaseline:  decimal.NewFromFloat(-2000.00),
			DebtPayoffCategoryID: &catID,
		}
		if err := NewRepo().Create(ctx, tx, &card); err != nil {
			return err
		}

		// Record $200 grocery spending this month on the revolver card
		now := time.Now().UTC()
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, amount, merchant_raw, transaction_date)
			VALUES ($1, $2, -200.00, 'Whole Foods', $3)
		`, wsID, card.ID, now); err != nil {
			return err
		}

		res, err := ProcessRevolverPayment(ctx, tx, card, decimal.NewFromFloat(600.00))
		if err != nil {
			return err
		}
		if res.TransferLegAmount.StringFixed(2) != "200.00" {
			t.Fatalf("TransferLegAmount = %s, want 200.00", res.TransferLegAmount.StringFixed(2))
		}
		if res.DebtLegAmount.StringFixed(2) != "400.00" {
			t.Fatalf("DebtLegAmount = %s, want 400.00", res.DebtLegAmount.StringFixed(2))
		}
		if res.DebtCategoryID != catID {
			t.Fatalf("DebtCategoryID = %s, want %s", res.DebtCategoryID, catID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("integration test failed: %v", err)
	}
}
