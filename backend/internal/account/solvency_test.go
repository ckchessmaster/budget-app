package account

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeSolvencyFromAccounts(t *testing.T) {
	t.Run("floating debt when pay-in-full credit card exceeds liquid cash", func(t *testing.T) {
		accounts := []Account{
			{
				Name:           "Checking",
				Type:           Checking,
				CurrentBalance: decimal.NewFromFloat(500.00),
				IsOnBudget:     true,
			},
			{
				Name:           "Pay-In-Full Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-800.00),
				IsOnBudget:     true,
				CardMode:       PayInFull,
			},
		}

		report := ComputeSolvencyFromAccounts(accounts)
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

	t.Run("ignores off-budget savings and DEBT_PAYDOWN cards", func(t *testing.T) {
		accounts := []Account{
			{
				Name:           "Checking",
				Type:           Checking,
				CurrentBalance: decimal.NewFromFloat(1000.00),
				IsOnBudget:     true,
			},
			{
				Name:           "On-Budget Savings",
				Type:           Savings,
				CurrentBalance: decimal.NewFromFloat(500.00),
				IsOnBudget:     true,
			},
			{
				Name:           "Off-Budget Stash",
				Type:           Savings,
				CurrentBalance: decimal.NewFromFloat(10000.00),
				IsOnBudget:     false,
			},
			{
				Name:           "Pay-In-Full Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-400.00),
				IsOnBudget:     true,
				CardMode:       PayInFull,
			},
			{
				Name:           "Revolver Debt Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-5000.00),
				CardMode:       DebtPaydown,
			},
		}

		report := ComputeSolvencyFromAccounts(accounts)
		if report.TotalLiquidCash.StringFixed(2) != "1500.00" {
			t.Fatalf("TotalLiquidCash = %s, want 1500.00", report.TotalLiquidCash.StringFixed(2))
		}
		if report.TotalCreditCardDebt.StringFixed(2) != "400.00" {
			t.Fatalf("TotalCreditCardDebt = %s, want 400.00", report.TotalCreditCardDebt.StringFixed(2))
		}
		if report.SolvencyBuffer.StringFixed(2) != "1100.00" {
			t.Fatalf("SolvencyBuffer = %s, want 1100.00", report.SolvencyBuffer.StringFixed(2))
		}
		if report.IsFloatingDebt {
			t.Fatalf("expected IsFloatingDebt = false")
		}
	})

	t.Run("includes on-budget cash and excludes off-budget cash and off-budget credit cards", func(t *testing.T) {
		accounts := []Account{
			{
				Name:           "Wallet Cash",
				Type:           Cash,
				CurrentBalance: decimal.NewFromFloat(250.00),
				IsOnBudget:     true,
			},
			{
				Name:           "Off-Budget Safe Cash",
				Type:           Cash,
				CurrentBalance: decimal.NewFromFloat(1000.00),
				IsOnBudget:     false,
			},
			{
				Name:           "Off-Budget Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-600.00),
				IsOnBudget:     false,
				CardMode:       PayInFull,
			},
			{
				Name:           "On-Budget Card",
				Type:           CreditCard,
				CurrentBalance: decimal.NewFromFloat(-100.00),
				IsOnBudget:     true,
				CardMode:       PayInFull,
			},
		}

		report := ComputeSolvencyFromAccounts(accounts)
		if report.TotalLiquidCash.StringFixed(2) != "250.00" {
			t.Fatalf("TotalLiquidCash = %s, want 250.00", report.TotalLiquidCash.StringFixed(2))
		}
		if report.TotalCreditCardDebt.StringFixed(2) != "100.00" {
			t.Fatalf("TotalCreditCardDebt = %s, want 100.00", report.TotalCreditCardDebt.StringFixed(2))
		}
		if report.SolvencyBuffer.StringFixed(2) != "150.00" {
			t.Fatalf("SolvencyBuffer = %s, want 150.00", report.SolvencyBuffer.StringFixed(2))
		}
	})
}
