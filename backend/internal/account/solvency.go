package account

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type SolvencyReport struct {
	TotalLiquidCash     decimal.Decimal `json:"total_liquid_cash"`
	TotalCreditCardDebt decimal.Decimal `json:"total_credit_card_debt"`
	SolvencyBuffer      decimal.Decimal `json:"solvency_buffer"`
	IsFloatingDebt      bool            `json:"is_floating_debt"`
	FloatingDebtAmount  decimal.Decimal `json:"floating_debt_amount"`
}

// ComputeSolvencyFromAccounts calculates the solvency buffer from a slice of accounts.
func ComputeSolvencyFromAccounts(accounts []Account) *SolvencyReport {
	totalLiquid := decimal.Zero
	totalCCDebt := decimal.Zero

	for _, a := range accounts {
		if (a.Type == Checking || a.Type == Savings || a.Type == Cash) && a.IsOnBudget {
			totalLiquid = totalLiquid.Add(a.CurrentBalance)
		}
		if a.Type == CreditCard && a.IsOnBudget && a.CardMode == PayInFull && a.CurrentBalance.IsNegative() {
			totalCCDebt = totalCCDebt.Add(a.CurrentBalance.Abs())
		}
	}

	totalLiquid = totalLiquid.Round(2)
	totalCCDebt = totalCCDebt.Round(2)
	buffer := totalLiquid.Sub(totalCCDebt).Round(2)

	report := &SolvencyReport{
		TotalLiquidCash:     totalLiquid,
		TotalCreditCardDebt: totalCCDebt,
		SolvencyBuffer:      buffer,
		IsFloatingDebt:      false,
		FloatingDebtAmount:  decimal.Zero.Round(2),
	}

	if buffer.IsNegative() {
		report.IsFloatingDebt = true
		report.FloatingDebtAmount = buffer.Abs().Round(2)
	}

	return report
}

// ComputeSolvencyReport queries workspace accounts within tx and computes the solvency report.
func ComputeSolvencyReport(ctx context.Context, tx pgx.Tx) (*SolvencyReport, error) {
	repo := NewRepo()
	accounts, err := repo.ListByWorkspace(ctx, tx)
	if err != nil {
		return nil, err
	}
	return ComputeSolvencyFromAccounts(accounts), nil
}
