package account

import (
	"time"

	"github.com/shopspring/decimal"
)

type DebtProjectionInput struct {
	CurrentBalance    decimal.Decimal
	MonthlyAllocation decimal.Decimal
	APRPercentage     decimal.Decimal
}

type DebtProjectionResult struct {
	MonthsToPayoff      int             `json:"months_to_payoff"`
	TotalInterestPaid   decimal.Decimal `json:"total_interest_paid"`
	ProjectedPayoffDate time.Time       `json:"projected_payoff_date"`
	IsUnpayable         bool            `json:"is_unpayable"`
}

const maxProjectionMonths = 1200

// CalculateDebtProjection simulates monthly compounding and principal reduction
// to project payoff duration and total interest paid.
func CalculateDebtProjection(input DebtProjectionInput) DebtProjectionResult {
	now := time.Now().UTC()
	balance := input.CurrentBalance.Abs()
	if balance.IsZero() {
		return DebtProjectionResult{
			MonthsToPayoff:      0,
			TotalInterestPaid:   decimal.Zero.Round(2),
			ProjectedPayoffDate: now,
			IsUnpayable:         false,
		}
	}

	if input.MonthlyAllocation.LessThanOrEqual(decimal.Zero) {
		return DebtProjectionResult{
			MonthsToPayoff:      0,
			TotalInterestPaid:   decimal.Zero.Round(2),
			ProjectedPayoffDate: now,
			IsUnpayable:         true,
		}
	}

	// r = APR / (100 * 12)
	monthlyRate := input.APRPercentage.Div(decimal.NewFromInt(1200))
	initialInterest := balance.Mul(monthlyRate)
	if input.MonthlyAllocation.LessThanOrEqual(initialInterest) {
		return DebtProjectionResult{
			MonthsToPayoff:      0,
			TotalInterestPaid:   decimal.Zero.Round(2),
			ProjectedPayoffDate: now,
			IsUnpayable:         true,
		}
	}

	totalInterest := decimal.Zero
	months := 0

	for balance.IsPositive() && months < maxProjectionMonths {
		interest := balance.Mul(monthlyRate)
		principal := input.MonthlyAllocation.Sub(interest)
		if principal.GreaterThan(balance) {
			// Final partial month: pay off remaining balance plus this month's interest
			totalInterest = totalInterest.Add(interest)
			balance = decimal.Zero
			months++
			break
		}
		totalInterest = totalInterest.Add(interest)
		balance = balance.Sub(principal)
		months++
	}

	if balance.IsPositive() {
		return DebtProjectionResult{
			MonthsToPayoff:      months,
			TotalInterestPaid:   totalInterest.Round(2),
			ProjectedPayoffDate: now.AddDate(0, months, 0),
			IsUnpayable:         true,
		}
	}

	return DebtProjectionResult{
		MonthsToPayoff:      months,
		TotalInterestPaid:   totalInterest.Round(2),
		ProjectedPayoffDate: now.AddDate(0, months, 0),
		IsUnpayable:         false,
	}
}
