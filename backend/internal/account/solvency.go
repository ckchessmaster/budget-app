package account

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func calculateSolvencyReport(accounts []Account) *SolvencyReport {
	liquidCash := decimal.Zero
	ccDebt := decimal.Zero

	for _, acct := range accounts {
		if (acct.Type == Checking || acct.Type == Savings) && acct.IsOnBudget {
			liquidCash = liquidCash.Add(acct.CurrentBalance)
		}
		if acct.Type == CreditCard && acct.CardMode == PayInFull && acct.CurrentBalance.LessThan(decimal.Zero) {
			ccDebt = ccDebt.Add(acct.CurrentBalance.Abs())
		}
	}

	buffer := liquidCash.Sub(ccDebt)
	isFloating := buffer.LessThan(decimal.Zero)
	floatingAmt := decimal.Zero
	if isFloating {
		floatingAmt = buffer.Abs()
	}

	return &SolvencyReport{
		TotalLiquidCash:     liquidCash.Round(2),
		TotalCreditCardDebt: ccDebt.Round(2),
		SolvencyBuffer:      buffer.Round(2),
		IsFloatingDebt:      isFloating,
		FloatingDebtAmount:  floatingAmt.Round(2),
	}
}

func ComputeSolvencyReport(ctx context.Context, tx pgx.Tx) (*SolvencyReport, error) {
	repo := NewAccountRepo()
	accounts, err := repo.ListByWorkspace(ctx, tx)
	if err != nil {
		return nil, err
	}
	return calculateSolvencyReport(accounts), nil
}
