package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type CategoryBudgetView struct {
	ID                    uuid.UUID        `json:"id"`
	Name                  string           `json:"name"`
	GroupName             string           `json:"group_name"`
	RolloverMode          RolloverMode     `json:"rollover_mode"`
	SweepTargetCategoryID *uuid.UUID       `json:"sweep_target_category_id"`
	TargetAmount          *decimal.Decimal `json:"target_amount"`
	TargetDate            *time.Time       `json:"target_date"`
	TargetCadence         string           `json:"target_cadence"`
	SortOrder             int              `json:"sort_order"`
	OriginalPlanned       decimal.Decimal  `json:"original_planned"`
	EffectiveTarget       decimal.Decimal  `json:"effective_target"`
	SpentCleared          decimal.Decimal  `json:"spent_cleared"`
	SpentPending          decimal.Decimal  `json:"spent_pending"`
	Remaining             decimal.Decimal  `json:"remaining"`
	Pacing                *PacingResult    `json:"pacing,omitempty"`
}

type MonthBudgetDetails struct {
	Summary    MonthSummary           `json:"summary"`
	Categories []CategoryBudgetView   `json:"categories"`
	Incomes    []MonthlyPlannedIncome `json:"incomes"`
}

type categoryMonthAmounts struct {
	EffectiveTarget decimal.Decimal
	SpentCleared    decimal.Decimal
	SpentPending    decimal.Decimal
}

func aggregateMonthSummary(yearMonth time.Time, totalIncome decimal.Decimal, items []categoryMonthAmounts) *MonthSummary {
	totalAllocated := decimal.Zero
	totalCleared := decimal.Zero
	totalPending := decimal.Zero
	isUnbalanced := false

	for _, item := range items {
		totalAllocated = totalAllocated.Add(item.EffectiveTarget)
		totalCleared = totalCleared.Add(item.SpentCleared)
		totalPending = totalPending.Add(item.SpentPending)

		remaining := item.EffectiveTarget.Sub(item.SpentCleared.Add(item.SpentPending))
		if remaining.LessThan(decimal.Zero) {
			isUnbalanced = true
		}
	}

	readyToAssign := totalIncome.Sub(totalAllocated)

	return &MonthSummary{
		YearMonth:           NormalizeYearMonth(yearMonth),
		TotalIncomeExpected: totalIncome.Round(2),
		TotalAllocated:      totalAllocated.Round(2),
		ReadyToAssign:       readyToAssign.Round(2),
		TotalSpentCleared:   totalCleared.Round(2),
		TotalSpentPending:   totalPending.Round(2),
		IsUnbalanced:        isUnbalanced,
	}
}

// ComputeMonthSummary computes the zero-based budget summary for a given month.
func ComputeMonthSummary(ctx context.Context, tx pgx.Tx, yearMonth time.Time) (*MonthSummary, error) {
	details, err := ComputeMonthDetails(ctx, tx, yearMonth)
	if err != nil {
		return nil, err
	}
	return &details.Summary, nil
}

// ComputeMonthDetails returns the full month summary, per-category breakdown, and planned incomes.
func ComputeMonthDetails(ctx context.Context, tx pgx.Tx, yearMonth time.Time) (*MonthBudgetDetails, error) {
	ym := NormalizeYearMonth(yearMonth)
	nextYM := ym.AddDate(0, 1, 0)

	// 1. Fetch planned incomes for the month
	incomeRepo := NewPlannedIncomeRepo()
	incomes, err := incomeRepo.ListByMonth(ctx, tx, ym)
	if err != nil {
		return nil, err
	}
	totalIncome := decimal.Zero
	for _, inc := range incomes {
		totalIncome = totalIncome.Add(inc.Amount)
	}

	// 2. Fetch categories with their baseline, net reallocations, and cleared/pending spend
	rows, err := tx.Query(ctx, `
		WITH cat_realloc AS (
			SELECT c.id AS category_id,
			       COALESCE(SUM(CASE WHEN r.to_category_id = c.id THEN r.amount ELSE 0 END), 0) -
			       COALESCE(SUM(CASE WHEN r.from_category_id = c.id THEN r.amount ELSE 0 END), 0) AS net_transfer
			FROM categories c
			LEFT JOIN budget_reallocations r
			       ON r.year_month = $1::date
			      AND (r.to_category_id = c.id OR r.from_category_id = c.id)
			GROUP BY c.id
		),
		cat_spend AS (
			SELECT category_id,
			       COALESCE(SUM(CASE WHEN status = 'POSTED' AND amount < 0 THEN -amount ELSE 0 END), 0) AS spent_cleared,
			       COALESCE(SUM(CASE WHEN status = 'PENDING' AND amount < 0 THEN -amount ELSE 0 END), 0) AS spent_pending
			FROM transactions
			WHERE category_id IS NOT NULL
			  AND is_transfer = FALSE
			  AND transaction_date >= $1::date
			  AND transaction_date < $2::date
			GROUP BY category_id
		)
		SELECT c.id,
		       c.name,
		       c.group_name,
		       c.rollover_mode,
		       c.sweep_target_category_id,
		       c.target_amount,
		       c.target_date,
		       c.target_cadence,
		       c.sort_order,
		       COALESCE(mcb.original_planned, c.monthly_budget) AS original_planned,
		       COALESCE(cr.net_transfer, 0) AS net_transfer,
		       COALESCE(cs.spent_cleared, 0) AS spent_cleared,
		       COALESCE(cs.spent_pending, 0) AS spent_pending
		FROM categories c
		LEFT JOIN monthly_category_budgets mcb
		       ON mcb.category_id = c.id
		      AND mcb.year_month = $1::date
		LEFT JOIN cat_realloc cr ON cr.category_id = c.id
		LEFT JOIN cat_spend cs ON cs.category_id = c.id
		ORDER BY c.sort_order ASC, c.group_name ASC, c.name ASC
	`, ym, nextYM)
	if err != nil {
		return nil, fmt.Errorf("failed to compute month details: %w", err)
	}
	defer rows.Close()

	categories := make([]CategoryBudgetView, 0)
	summaryItems := make([]categoryMonthAmounts, 0)

	for rows.Next() {
		var v CategoryBudgetView
		var rolloverMode string
		var netTransfer decimal.Decimal
		if err := rows.Scan(
			&v.ID, &v.Name, &v.GroupName, &rolloverMode,
			&v.SweepTargetCategoryID, &v.TargetAmount, &v.TargetDate,
			&v.TargetCadence, &v.SortOrder,
			&v.OriginalPlanned, &netTransfer, &v.SpentCleared, &v.SpentPending,
		); err != nil {
			return nil, fmt.Errorf("failed to scan category budget view: %w", err)
		}
		v.RolloverMode = RolloverMode(rolloverMode)
		v.EffectiveTarget = v.OriginalPlanned.Add(netTransfer).Round(2)
		v.Remaining = v.EffectiveTarget.Sub(v.SpentCleared.Add(v.SpentPending)).Round(2)

		if v.TargetAmount != nil && v.TargetDate != nil {
			pacing := CalculateSinkingFundPace(PacingInput{
				TargetAmount:   *v.TargetAmount,
				CurrentBalance: v.Remaining,
				TargetDate:     *v.TargetDate,
				CurrentDate:    ym,
			})
			v.Pacing = &pacing
		}

		categories = append(categories, v)
		summaryItems = append(summaryItems, categoryMonthAmounts{
			EffectiveTarget: v.EffectiveTarget,
			SpentCleared:    v.SpentCleared,
			SpentPending:    v.SpentPending,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating category budget views: %w", err)
	}

	summary := aggregateMonthSummary(ym, totalIncome, summaryItems)
	return &MonthBudgetDetails{
		Summary:    *summary,
		Categories: categories,
		Incomes:    incomes,
	}, nil
}
