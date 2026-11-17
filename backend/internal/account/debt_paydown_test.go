package account

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestCalculateDebtProjection(t *testing.T) {
	t.Run("$4500 debt at 24% APR paying $400/month -> ~13-14 months", func(t *testing.T) {
		res := CalculateDebtProjection(DebtProjectionInput{
			CurrentBalance:    decimal.NewFromFloat(-4500.00),
			MonthlyAllocation: decimal.NewFromFloat(400.00),
			APRPercentage:     decimal.NewFromFloat(24.0),
		})
		if res.IsUnpayable {
			t.Fatalf("expected payable debt, got IsUnpayable = true")
		}
		if res.MonthsToPayoff < 13 || res.MonthsToPayoff > 14 {
			t.Fatalf("MonthsToPayoff = %d, want 13 or 14", res.MonthsToPayoff)
		}
		if !res.TotalInterestPaid.GreaterThan(decimal.NewFromFloat(600.00)) {
			t.Fatalf("expected TotalInterestPaid > 600, got %s", res.TotalInterestPaid.StringFixed(2))
		}
	})

	t.Run("allocation <= monthly interest is unpayable", func(t *testing.T) {
		res := CalculateDebtProjection(DebtProjectionInput{
			CurrentBalance:    decimal.NewFromFloat(-5000.00),
			MonthlyAllocation: decimal.NewFromFloat(100.00), // 24% APR on $5000 = $100/mo interest
			APRPercentage:     decimal.NewFromFloat(24.0),
		})
		if !res.IsUnpayable {
			t.Fatalf("expected IsUnpayable = true")
		}
	})
}
