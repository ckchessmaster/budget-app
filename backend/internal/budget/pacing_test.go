package budget

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCalculateSinkingFundPace(t *testing.T) {
	tests := []struct {
		name           string
		input          PacingInput
		wantMonthlyDue string
		wantMonths     int
		wantIsMet      bool
	}{
		{
			name: "Case A: Target $1200, Balance $0, 12 months left",
			input: PacingInput{
				TargetAmount:   decimal.NewFromInt(1200),
				CurrentBalance: decimal.Zero,
				CurrentDate:    time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
				TargetDate:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantMonthlyDue: "100.00",
			wantMonths:     12,
			wantIsMet:      false,
		},
		{
			name: "Case B: Target $1000, Balance $400, 3 months left",
			input: PacingInput{
				TargetAmount:   decimal.NewFromInt(1000),
				CurrentBalance: decimal.NewFromInt(400),
				CurrentDate:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
				TargetDate:     time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			},
			wantMonthlyDue: "200.00",
			wantMonths:     3,
			wantIsMet:      false,
		},
		{
			name: "Case C: Target $500, Balance $600, 5 months left (met)",
			input: PacingInput{
				TargetAmount:   decimal.NewFromInt(500),
				CurrentBalance: decimal.NewFromInt(600),
				CurrentDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				TargetDate:     time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			},
			wantMonthlyDue: "0.00",
			wantMonths:     5,
			wantIsMet:      true,
		},
		{
			name: "Ceiling rounding to 2 decimals: $100 over 3 months -> $33.34",
			input: PacingInput{
				TargetAmount:   decimal.NewFromInt(100),
				CurrentBalance: decimal.Zero,
				CurrentDate:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TargetDate:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			},
			wantMonthlyDue: "33.34",
			wantMonths:     3,
			wantIsMet:      false,
		},
		{
			name: "Past due / 0 months remaining -> full remaining due",
			input: PacingInput{
				TargetAmount:   decimal.NewFromInt(500),
				CurrentBalance: decimal.NewFromInt(150),
				CurrentDate:    time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
				TargetDate:     time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			},
			wantMonthlyDue: "350.00",
			wantMonths:     0,
			wantIsMet:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateSinkingFundPace(tc.input)
			if got.MonthlyDue.StringFixed(2) != tc.wantMonthlyDue {
				t.Errorf("MonthlyDue = %s, want %s", got.MonthlyDue.StringFixed(2), tc.wantMonthlyDue)
			}
			if got.MonthsRemaining != tc.wantMonths {
				t.Errorf("MonthsRemaining = %d, want %d", got.MonthsRemaining, tc.wantMonths)
			}
			if got.IsMet != tc.wantIsMet {
				t.Errorf("IsMet = %v, want %v", got.IsMet, tc.wantIsMet)
			}
		})
	}
}
