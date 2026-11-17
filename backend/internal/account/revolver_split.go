package account

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func computeRevolverSplit(
	activeSpendThisCycle decimal.Decimal,
	paymentAmount decimal.Decimal,
	debtCategoryID uuid.UUID,
) *PaymentSplitResult {
	transferLeg := paymentAmount
	if activeSpendThisCycle.LessThan(transferLeg) {
		transferLeg = activeSpendThisCycle
	}
	if transferLeg.LessThan(decimal.Zero) {
		transferLeg = decimal.Zero
	}
	debtLeg := paymentAmount.Sub(transferLeg)

	return &PaymentSplitResult{
		TransferLegAmount: transferLeg.Round(2),
		DebtLegAmount:     debtLeg.Round(2),
		DebtCategoryID:    debtCategoryID,
	}
}

// ProcessRevolverPayment splits a payment to a DEBT_PAYDOWN credit card into:
// 1) TransferLegAmount (reimburses new budgeted spending in the current cycle)
// 2) DebtLegAmount (allocated to DebtPayoffCategoryID to reduce pre-existing principal).
func ProcessRevolverPayment(
	ctx context.Context,
	tx pgx.Tx,
	cardAccount Account,
	paymentAmount decimal.Decimal,
) (*PaymentSplitResult, error) {
	if cardAccount.Type != CreditCard || cardAccount.CardMode != DebtPaydown {
		return nil, ErrInvalidCardMode
	}
	if cardAccount.DebtPayoffCategoryID == nil || *cardAccount.DebtPayoffCategoryID == uuid.Nil {
		return nil, ErrMissingDebtCategory
	}
	if !paymentAmount.GreaterThan(decimal.Zero) {
		return nil, ErrInvalidAmount
	}

	var activeSpend decimal.Decimal
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(ABS(amount)), 0)
		FROM transactions
		WHERE account_id = $1
		  AND amount < 0
		  AND is_transfer = FALSE
		  AND transaction_date >= COALESCE(
			(SELECT MAX(transaction_date)
			 FROM transactions
			 WHERE account_id = $1 AND amount > 0 AND is_transfer = TRUE),
			'1970-01-01'::date
		  )
	`, cardAccount.ID).Scan(&activeSpend)
	if err != nil {
		return nil, fmt.Errorf("failed to query active spend for revolver split: %w", err)
	}

	return computeRevolverSplit(activeSpend, paymentAmount, *cardAccount.DebtPayoffCategoryID), nil
}
