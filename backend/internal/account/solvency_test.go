package account

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeSolvencyReport(t *testing.T) {
	t.Run("Checking $500, Card -$800 -> Solvency Buffer -$300, IsFloatingDebt = true", func(t *testing.T) {
		report := calculateSolvencyReport([]Account{
			{
				Name:           "Checking",
				Type:           Checking,
				CurrentBalance: decimal.NewFromFloat(500.00),
				IsOnBudget:     true,
			},
			{
				Name:           "Chase Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-800.00),
				IsOnBudget:     true,
				CardMode:       PayInFull,
			},
		})

		if report.TotalLiquidCash.StringFixed(2) != "500.00" {
			t.Fatalf("TotalLiquidCash = %s, want 500.00", report.TotalLiquidCash.StringFixed(2))
		}
		if report.TotalCreditCardDebt.StringFixed(2) != "800.00" {
			t.Fatalf("TotalCreditCardDebt = %s, want 800.00", report.TotalCreditCardDebt.StringFixed(2))
		}
		if report.SolvencyBuffer.StringFixed(2) != "-300.00" {
			t.Fatalf("SolvencyBuffer = %s, want -300.00", report.SolvencyBuffer.StringFixed(2))
		}
		if !report.IsFloatingDebt {
			t.Fatalf("expected IsFloatingDebt = true")
		}
		if report.FloatingDebtAmount.StringFixed(2) != "300.00" {
			t.Fatalf("FloatingDebtAmount = %s, want 300.00", report.FloatingDebtAmount.StringFixed(2))
		}
	})

	t.Run("ignores DEBT_PAYDOWN cards and off-budget accounts", func(t *testing.T) {
		report := calculateSolvencyReport([]Account{
			{
				Name:           "Checking",
				Type:           Checking,
				CurrentBalance: decimal.NewFromFloat(1200.00),
				IsOnBudget:     true,
			},
			{
				Name:           "Off-Budget Savings",
				Type:           Savings,
				CurrentBalance: decimal.NewFromFloat(5000.00),
				IsOnBudget:     false,
			},
			{
				Name:           "Revolver Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-3000.00),
				IsOnBudget:     true,
				CardMode:       DebtPaydown,
			},
		})

		if report.SolvencyBuffer.StringFixed(2) != "1200.00" {
			t.Fatalf("SolvencyBuffer = %s, want 1200.00", report.SolvencyBuffer.StringFixed(2))
		}
		if report.IsFloatingDebt {
			t.Fatalf("expected IsFloatingDebt = false")
		}
	})
}
