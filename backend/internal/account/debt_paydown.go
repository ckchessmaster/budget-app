package account

import (
	"time"

	"github.com/shopspring/decimal"
)

// CalculateDebtProjection iteratively simulates monthly credit card amortization
// given CurrentBalance, MonthlyAllocation, and APRPercentage.
func CalculateDebtProjection(input DebtProjectionInput) DebtProjectionResult {
	balance := input.CurrentBalance.Abs()
	now := time.Now().UTC()
	baseMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	if balance.IsZero() {
		return DebtProjectionResult{
			MonthsToPayoff:      0,
			TotalInterestPaid:   decimal.Zero.Round(2),
			ProjectedPayoffDate: baseMonth,
			IsUnpayable:         false,
		}
	}

	monthlyRate := input.APRPercentage.Div(decimal.NewFromInt(1200))
	initialInterest := balance.Mul(monthlyRate).Round(2)

	if !input.MonthlyAllocation.GreaterThan(initialInterest) {
		return DebtProjectionResult{
			MonthsToPayoff:      0,
			TotalInterestPaid:   decimal.Zero.Round(2),
			ProjectedPayoffDate: baseMonth,
			IsUnpayable:         true,
		}
	}

	totalInterest := decimal.Zero
	months := 0
	const maxMonths = 1200 // 100-year safety bound

	for balance.GreaterThan(decimal.Zero) && months < maxMonths {
		interest := balance.Mul(monthlyRate).Round(2)
		totalInterest = totalInterest.Add(interest)

		if balance.Add(interest).LessThanOrEqual(input.MonthlyAllocation) {
			balance = decimal.Zero
			months++
			break
		}

		principal := input.MonthlyAllocation.Sub(interest)
		balance = balance.Sub(principal)
		months++
	}

	return DebtProjectionResult{
		MonthsToPayoff:      months,
		TotalInterestPaid:   totalInterest.Round(2),
		ProjectedPayoffDate: baseMonth.AddDate(0, months, 0),
		IsUnpayable:         false,
	}
}
