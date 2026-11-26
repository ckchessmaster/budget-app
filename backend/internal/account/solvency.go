package account

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type ReservedFundItem struct {
	CategoryID uuid.UUID       `json:"category_id"`
	Name       string          `json:"name"`
	Balance    decimal.Decimal `json:"balance"`
}

type AccountFundReservation struct {
	AccountID     uuid.UUID          `json:"account_id"`
	ReservedTotal decimal.Decimal    `json:"reserved_total"`
	Funds         []ReservedFundItem `json:"funds"`
}

type SolvencyReport struct {
	TotalLiquidCash      decimal.Decimal          `json:"total_liquid_cash"`
	TotalCreditCardDebt  decimal.Decimal          `json:"total_credit_card_debt"`
	SolvencyBuffer       decimal.Decimal          `json:"solvency_buffer"`
	IsFloatingDebt       bool                     `json:"is_floating_debt"`
	FloatingDebtAmount   decimal.Decimal          `json:"floating_debt_amount"`
	TotalReservedInFunds decimal.Decimal          `json:"total_reserved_in_funds"`
	UnreservedCash       decimal.Decimal          `json:"unreserved_cash"`
	AccountReservations  []AccountFundReservation `json:"account_reservations"`
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
		TotalLiquidCash:      totalLiquid,
		TotalCreditCardDebt:  totalCCDebt,
		SolvencyBuffer:       buffer,
		IsFloatingDebt:       false,
		FloatingDebtAmount:   decimal.Zero.Round(2),
		TotalReservedInFunds: decimal.Zero.Round(2),
		UnreservedCash:       buffer,
		AccountReservations:  []AccountFundReservation{},
	}

	if buffer.IsNegative() {
		report.IsFloatingDebt = true
		report.FloatingDebtAmount = buffer.Abs().Round(2)
	}

	return report
}

// ComputeSolvencyReport queries workspace accounts and active sinking fund balances within tx
// and computes the solvency report including per-account fund reservations.
func ComputeSolvencyReport(ctx context.Context, tx pgx.Tx) (*SolvencyReport, error) {
	repo := NewRepo()
	accounts, err := repo.ListByWorkspace(ctx, tx)
	if err != nil {
		return nil, err
	}
	report := ComputeSolvencyFromAccounts(accounts)

	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ym := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	nextYM := ym.AddDate(0, 1, 0)

	rows, err := tx.Query(ctx, `
		WITH cat_realloc AS (
			SELECT c.id AS category_id,
			       COALESCE(SUM(CASE WHEN r.to_category_id = c.id THEN r.amount ELSE 0 END), 0) -
			       COALESCE(SUM(CASE WHEN r.from_category_id = c.id THEN r.amount ELSE 0 END), 0) AS net_transfer
			FROM categories c
			LEFT JOIN budget_reallocations r
			       ON r.workspace_id = $3
			      AND r.year_month = $1::date
			      AND (r.to_category_id = c.id OR r.from_category_id = c.id)
			WHERE c.workspace_id = $3
			  AND c.rollover_mode <> 'SWEEP'
			GROUP BY c.id
		),
		cat_spend AS (
			SELECT t.category_id,
			       COALESCE(SUM(-t.amount), 0) AS net_spent
			FROM transactions t
			INNER JOIN accounts a ON a.id = t.account_id
			WHERE t.workspace_id = $3
			  AND a.workspace_id = $3
			  AND t.category_id IS NOT NULL
			  AND t.is_transfer = FALSE
			  AND a.is_on_budget = TRUE
			  AND COALESCE(t.budget_month, date_trunc('month', t.transaction_date)::date) >= $1::date
			  AND COALESCE(t.budget_month, date_trunc('month', t.transaction_date)::date) < $2::date
			GROUP BY t.category_id
		)
		SELECT c.id,
		       c.name,
		       c.held_in_account_id,
		       COALESCE(mcb.carryover_balance, 0) +
		       COALESCE(mcb.original_planned, c.monthly_budget) +
		       COALESCE(cr.net_transfer, 0) -
		       COALESCE(cs.net_spent, 0) AS balance
		FROM categories c
		LEFT JOIN monthly_category_budgets mcb
		       ON mcb.workspace_id = $3
		      AND mcb.category_id = c.id
		      AND mcb.year_month = $1::date
		LEFT JOIN cat_realloc cr ON cr.category_id = c.id
		LEFT JOIN cat_spend cs ON cs.category_id = c.id
		WHERE c.workspace_id = $3
		  AND c.rollover_mode <> 'SWEEP'
		ORDER BY c.sort_order ASC, c.name ASC
	`, ym, nextYM, wsID)
	if err != nil {
		return nil, fmt.Errorf("failed to compute fund reservations: %w", err)
	}
	defer rows.Close()

	totalReserved := decimal.Zero
	byAccount := make(map[uuid.UUID]*AccountFundReservation)
	for rows.Next() {
		var catID uuid.UUID
		var catName string
		var heldAccountID *uuid.UUID
		var rawBalance decimal.Decimal
		if err := rows.Scan(&catID, &catName, &heldAccountID, &rawBalance); err != nil {
			return nil, fmt.Errorf("failed to scan fund reservation: %w", err)
		}
		bal := rawBalance.Round(2)
		if !bal.GreaterThan(decimal.Zero) {
			bal = decimal.Zero.Round(2)
		}
		totalReserved = totalReserved.Add(bal)

		if heldAccountID != nil && *heldAccountID != uuid.Nil {
			res, ok := byAccount[*heldAccountID]
			if !ok {
				res = &AccountFundReservation{
					AccountID:     *heldAccountID,
					ReservedTotal: decimal.Zero.Round(2),
					Funds:         []ReservedFundItem{},
				}
				byAccount[*heldAccountID] = res
			}
			res.ReservedTotal = res.ReservedTotal.Add(bal).Round(2)
			res.Funds = append(res.Funds, ReservedFundItem{
				CategoryID: catID,
				Name:       catName,
				Balance:    bal,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating fund reservations: %w", err)
	}

	report.TotalReservedInFunds = totalReserved.Round(2)
	report.UnreservedCash = report.SolvencyBuffer.Sub(report.TotalReservedInFunds).Round(2)

	reservations := make([]AccountFundReservation, 0, len(byAccount))
	for _, r := range byAccount {
		reservations = append(reservations, *r)
	}
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].AccountID.String() < reservations[j].AccountID.String()
	})
	report.AccountReservations = reservations

	return report, nil
}
