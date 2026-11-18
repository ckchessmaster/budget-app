package account

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestCalculateDebtProjection(t *testing.T) {
	t.Run("4500 debt at 24% APR paying 400/month", func(t *testing.T) {
		res := CalculateDebtProjection(DebtProjectionInput{
			CurrentBalance:    decimal.NewFromFloat(-4500.00),
			MonthlyAllocation: decimal.NewFromFloat(400.00),
			APRPercentage:     decimal.NewFromFloat(24.00),
		})
		if res.IsUnpayable {
			t.Fatalf("expected IsUnpayable = false")
		}
		if res.MonthsToPayoff != 13 && res.MonthsToPayoff != 14 {
			t.Fatalf("MonthsToPayoff = %d, expected ~13-14", res.MonthsToPayoff)
		}
		if res.TotalInterestPaid.LessThan(decimal.NewFromFloat(550)) || res.TotalInterestPaid.GreaterThan(decimal.NewFromFloat(700)) {
			t.Fatalf("unexpected TotalInterestPaid = %s", res.TotalInterestPaid.StringFixed(2))
		}
	})

	t.Run("marks unpayable when allocation does not exceed monthly interest", func(t *testing.T) {
		res := CalculateDebtProjection(DebtProjectionInput{
			CurrentBalance:    decimal.NewFromFloat(-5000.00),
			MonthlyAllocation: decimal.NewFromFloat(100.00),
			APRPercentage:     decimal.NewFromFloat(24.00),
		})
		if !res.IsUnpayable {
			t.Fatalf("expected IsUnpayable = true when $100 <= $100 monthly interest")
		}
	})

	t.Run("zero balance pays off in 0 months", func(t *testing.T) {
		res := CalculateDebtProjection(DebtProjectionInput{
			CurrentBalance:    decimal.Zero,
			MonthlyAllocation: decimal.NewFromFloat(200.00),
			APRPercentage:     decimal.NewFromFloat(18.00),
		})
		if res.IsUnpayable || res.MonthsToPayoff != 0 {
			t.Fatalf("unexpected result for zero balance: %+v", res)
		}
	})
}
