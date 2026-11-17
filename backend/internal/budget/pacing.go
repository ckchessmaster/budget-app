package budget

import (
	"time"

	"github.com/shopspring/decimal"
)

type PacingInput struct {
	TargetAmount   decimal.Decimal
	CurrentBalance decimal.Decimal
	TargetDate     time.Time
	CurrentDate    time.Time
}

type PacingResult struct {
	MonthlyDue      decimal.Decimal `json:"monthly_due"`
	MonthsRemaining int             `json:"months_remaining"`
	IsMet           bool            `json:"is_met"`
}

// CalculateSinkingFundPace computes the monthly contribution required to reach
// TargetAmount by TargetDate given CurrentBalance at CurrentDate.
func CalculateSinkingFundPace(input PacingInput) PacingResult {
	monthsRemaining := (input.TargetDate.Year()-input.CurrentDate.Year())*12 +
		int(input.TargetDate.Month()-input.CurrentDate.Month())

	if input.CurrentBalance.GreaterThanOrEqual(input.TargetAmount) {
		return PacingResult{
			MonthlyDue:      decimal.Zero.Round(2),
			MonthsRemaining: monthsRemaining,
			IsMet:           true,
		}
	}

	diff := input.TargetAmount.Sub(input.CurrentBalance)
	if monthsRemaining <= 0 {
		if diff.LessThan(decimal.Zero) {
			diff = decimal.Zero
		}
		return PacingResult{
			MonthlyDue:      diff.Round(2),
			MonthsRemaining: monthsRemaining,
			IsMet:           false,
		}
	}

	// Ceiling to 2 decimal places using exact integer division on cents.
	cents := diff.Mul(decimal.NewFromInt(100))
	divisor := decimal.NewFromInt(int64(monthsRemaining))
	q, r := cents.QuoRem(divisor, 0)
	if !r.IsZero() {
		q = q.Add(decimal.NewFromInt(1))
	}
	monthlyDue := q.Div(decimal.NewFromInt(100)).Round(2)

	return PacingResult{
		MonthlyDue:      monthlyDue,
		MonthsRemaining: monthsRemaining,
		IsMet:           false,
	}
}
