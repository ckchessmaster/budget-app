package account

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

var (
	ErrNotDebtPaydownCard   = errors.New("revolver payment split only applies to credit_card accounts in DEBT_PAYDOWN mode")
	ErrInvalidPaymentAmount = errors.New("payment amount must be positive")
)

type PaymentSplitResult struct {
	TransferLegAmount decimal.Decimal `json:"transfer_leg_amount"`
	DebtLegAmount     decimal.Decimal `json:"debt_leg_amount"`
	DebtCategoryID    uuid.UUID       `json:"debt_category_id"`
}

// CalculateRevolverSplit computes the transfer leg (reimbursing budgeted active spend)
// and the debt reduction leg (allocated to DebtPayoffCategoryID).
func CalculateRevolverSplit(
	activeSpendThisCycle decimal.Decimal,
	paymentAmount decimal.Decimal,
	debtCategoryID uuid.UUID,
) *PaymentSplitResult {
	active := activeSpendThisCycle.Abs().Round(2)
	payment := paymentAmount.Round(2)

	transferLeg := payment
	if active.LessThan(payment) {
		transferLeg = active
	}
	debtLeg := payment.Sub(transferLeg).Round(2)

	return &PaymentSplitResult{
		TransferLegAmount: transferLeg.Round(2),
		DebtLegAmount:     debtLeg,
		DebtCategoryID:    debtCategoryID,
	}
}

// ProcessRevolverPayment queries active debit spending on cardAccount since the last
// payment (or start of the current month) and returns the split between the neutral
// transfer leg and the debt paydown category leg.
func ProcessRevolverPayment(
	ctx context.Context,
	tx pgx.Tx,
	cardAccount Account,
	paymentAmount decimal.Decimal,
) (*PaymentSplitResult, error) {
	if cardAccount.Type != CreditCard || cardAccount.CardMode != DebtPaydown {
		return nil, ErrNotDebtPaydownCard
	}
	if cardAccount.DebtPayoffCategoryID == nil || *cardAccount.DebtPayoffCategoryID == uuid.Nil {
		return nil, ErrMissingDebtPayoffCat
	}
	if paymentAmount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidPaymentAmount
	}

	now := time.Now().UTC()
	cycleStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var lastPaymentDate *time.Time
	err := tx.QueryRow(ctx, `
		SELECT MAX(transaction_date)
		FROM transactions
		WHERE account_id = $1
		  AND parent_transaction_id IS NULL
		  AND amount > 0
	`, cardAccount.ID).Scan(&lastPaymentDate)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	sinceDate := cycleStart
	if lastPaymentDate != nil && lastPaymentDate.After(cycleStart) {
		sinceDate = *lastPaymentDate
	}

	var activeSpend decimal.Decimal
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(ABS(amount)), 0)
		FROM transactions
		WHERE account_id = $1
		  AND parent_transaction_id IS NULL
		  AND amount < 0
		  AND is_transfer = FALSE
		  AND transaction_date >= $2
	`, cardAccount.ID, sinceDate).Scan(&activeSpend)
	if err != nil {
		return nil, err
	}

	return CalculateRevolverSplit(activeSpend, paymentAmount, *cardAccount.DebtPayoffCategoryID), nil
}
