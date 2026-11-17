package account

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestProcessRevolverPayment(t *testing.T) {
	debtCatID := uuid.New()

	t.Run("$200 active spend and $600 payment -> Transfer $200.00, Debt $400.00", func(t *testing.T) {
		res := computeRevolverSplit(
			decimal.NewFromFloat(200.00),
			decimal.NewFromFloat(600.00),
			debtCatID,
		)
		if res.TransferLegAmount.StringFixed(2) != "200.00" {
			t.Fatalf("TransferLegAmount = %s, want 200.00", res.TransferLegAmount.StringFixed(2))
		}
		if res.DebtLegAmount.StringFixed(2) != "400.00" {
			t.Fatalf("DebtLegAmount = %s, want 400.00", res.DebtLegAmount.StringFixed(2))
		}
		if res.DebtCategoryID != debtCatID {
			t.Fatalf("DebtCategoryID = %v, want %v", res.DebtCategoryID, debtCatID)
		}
	})

	t.Run("payment smaller than active spend -> 100% Transfer Leg, $0 Debt Leg", func(t *testing.T) {
		res := computeRevolverSplit(
			decimal.NewFromFloat(350.00),
			decimal.NewFromFloat(150.00),
			debtCatID,
		)
		if res.TransferLegAmount.StringFixed(2) != "150.00" {
			t.Fatalf("TransferLegAmount = %s, want 150.00", res.TransferLegAmount.StringFixed(2))
		}
		if res.DebtLegAmount.StringFixed(2) != "0.00" {
			t.Fatalf("DebtLegAmount = %s, want 0.00", res.DebtLegAmount.StringFixed(2))
		}
	})
}
