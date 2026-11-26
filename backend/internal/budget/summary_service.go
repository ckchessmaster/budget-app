package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
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
	HeldInAccountID       *uuid.UUID       `json:"held_in_account_id"`
	TargetAmount          *decimal.Decimal `json:"target_amount"`
	TargetDate            *time.Time       `json:"target_date"`
	TargetCadence           string           `json:"target_cadence"`
	SortOrder               int              `json:"sort_order"`
	BudgetAlertEnabled      bool             `json:"budget_alert_enabled"`
	BudgetAlertThresholdPct int              `json:"budget_alert_threshold_pct"`
	OriginalPlanned         decimal.Decimal  `json:"original_planned"`
	CarryoverBalance      decimal.Decimal  `json:"carryover_balance"`
	EffectiveTarget       decimal.Decimal  `json:"effective_target"`
	SpentCleared          decimal.Decimal  `json:"spent_cleared"`
	SpentPending          decimal.Decimal  `json:"spent_pending"`
	Remaining             decimal.Decimal  `json:"remaining"`
	Pacing                *PacingResult    `json:"pacing,omitempty"`
}

type MonthBudgetDetails struct {
	Summary           MonthSummary           `json:"summary"`
	Categories        []CategoryBudgetView   `json:"categories"`
	Incomes           []MonthlyPlannedIncome `json:"incomes"`
	PendingInterlocks []CCDebtInterlockView  `json:"pending_interlocks,omitempty"`
}

type categoryMonthAmounts struct {
	CarryoverBalance decimal.Decimal
	EffectiveTarget  decimal.Decimal
	SpentCleared     decimal.Decimal
	SpentPending     decimal.Decimal
}

func aggregateMonthSummary(yearMonth time.Time, totalIncome decimal.Decimal, rolloverAdjustment decimal.Decimal, items []categoryMonthAmounts) *MonthSummary {
	totalAllocated := decimal.Zero
	totalCleared := decimal.Zero
	totalPending := decimal.Zero
	isUnbalanced := false

	for _, item := range items {
		totalAllocated = totalAllocated.Add(item.EffectiveTarget)
		totalCleared = totalCleared.Add(item.SpentCleared)
		totalPending = totalPending.Add(item.SpentPending)

		remaining := item.CarryoverBalance.Add(item.EffectiveTarget).Sub(item.SpentCleared.Add(item.SpentPending))
		if remaining.LessThan(decimal.Zero) {
			isUnbalanced = true
		}
	}

	readyToAssign := totalIncome.Add(rolloverAdjustment).Sub(totalAllocated)

	return &MonthSummary{
		YearMonth:           NormalizeYearMonth(yearMonth),
		TotalIncomeExpected: totalIncome.Round(2),
		RolloverAdjustment:  rolloverAdjustment.Round(2),
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

func loadMonthCategoryViews(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, ym time.Time) ([]CategoryBudgetView, error) {
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
			GROUP BY c.id
		),
		cat_spend AS (
			SELECT t.category_id,
			       COALESCE(SUM(CASE WHEN t.status = 'POSTED' THEN -t.amount ELSE 0 END), 0) AS spent_cleared,
			       COALESCE(SUM(CASE WHEN t.status = 'PENDING' THEN -t.amount ELSE 0 END), 0) AS spent_pending
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
		       c.group_name,
		       c.rollover_mode,
		       c.sweep_target_category_id,
		       c.held_in_account_id,
		       c.target_amount,
		       c.target_date,
		       c.target_cadence,
		       c.sort_order,
		       c.budget_alert_enabled,
		       c.budget_alert_threshold_pct,
		       COALESCE(mcb.original_planned, c.monthly_budget) AS original_planned,
		       COALESCE(mcb.carryover_balance, 0) AS carryover_balance,
		       COALESCE(cr.net_transfer, 0) AS net_transfer,
		       COALESCE(cs.spent_cleared, 0) AS spent_cleared,
		       COALESCE(cs.spent_pending, 0) AS spent_pending
		FROM categories c
		LEFT JOIN monthly_category_budgets mcb
		       ON mcb.workspace_id = $3
		      AND mcb.category_id = c.id
		      AND mcb.year_month = $1::date
		LEFT JOIN cat_realloc cr ON cr.category_id = c.id
		LEFT JOIN cat_spend cs ON cs.category_id = c.id
		WHERE c.workspace_id = $3
		ORDER BY c.sort_order ASC, c.group_name ASC, c.name ASC
	`, ym, nextYM, wsID)
	if err != nil {
		return nil, fmt.Errorf("failed to compute month details: %w", err)
	}
	defer rows.Close()

	categories := make([]CategoryBudgetView, 0)
	for rows.Next() {
		var v CategoryBudgetView
		var rolloverMode string
		var netTransfer decimal.Decimal
		if err := rows.Scan(
			&v.ID, &v.Name, &v.GroupName, &rolloverMode,
			&v.SweepTargetCategoryID, &v.HeldInAccountID, &v.TargetAmount, &v.TargetDate,
			&v.TargetCadence, &v.SortOrder,
			&v.BudgetAlertEnabled, &v.BudgetAlertThresholdPct,
			&v.OriginalPlanned, &v.CarryoverBalance, &netTransfer, &v.SpentCleared, &v.SpentPending,
		); err != nil {
			return nil, fmt.Errorf("failed to scan category budget view: %w", err)
		}
		v.RolloverMode = RolloverMode(rolloverMode)
		v.CarryoverBalance = v.CarryoverBalance.Round(2)
		v.EffectiveTarget = v.OriginalPlanned.Add(netTransfer).Round(2)
		v.Remaining = v.CarryoverBalance.Add(v.EffectiveTarget).Sub(v.SpentCleared.Add(v.SpentPending)).Round(2)
		categories = append(categories, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating category budget views: %w", err)
	}
	return categories, nil
}

// projectFutureCarryover walks months from currentYM up to targetYM-1 and computes projected
// carryover balances for future months before the month-end rollover job has executed.
func projectFutureCarryover(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, currentYM, targetYM time.Time) (map[uuid.UUID]decimal.Decimal, error) {
	projected := make(map[uuid.UUID]decimal.Decimal)
	m := currentYM
	for m.Before(targetYM) {
		nextM := m.AddDate(0, 1, 0)
		var rolloverRan bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM month_end_rollover_runs
				WHERE workspace_id = $1 AND target_month = $2::date
			)
		`, wsID, nextM).Scan(&rolloverRan); err != nil {
			return nil, fmt.Errorf("failed to check rollover run for %s: %w", nextM.Format("2006-01"), err)
		}

		views, err := loadMonthCategoryViews(ctx, tx, wsID, m)
		if err != nil {
			return nil, err
		}

		if rolloverRan {
			// Next month already has materialized carryover_balance in DB; clear carried projection.
			clearMap := make(map[uuid.UUID]decimal.Decimal)
			projected = clearMap
			m = nextM
			continue
		}

		inputs := make([]CategoryRolloverInput, len(views))
		for i, v := range views {
			carry := v.CarryoverBalance
			if carry.IsZero() {
				if p, ok := projected[v.ID]; ok {
					carry = p
				}
			}
			rem := carry.Add(v.EffectiveTarget).Sub(v.SpentCleared.Add(v.SpentPending)).Round(2)
			inputs[i] = CategoryRolloverInput{
				Category: Category{
					ID:                    v.ID,
					WorkspaceID:           wsID,
					Name:                  v.Name,
					GroupName:             v.GroupName,
					MonthlyBudget:         v.OriginalPlanned,
					RolloverMode:          v.RolloverMode,
					SweepTargetCategoryID: v.SweepTargetCategoryID,
					TargetAmount:          v.TargetAmount,
					TargetDate:            v.TargetDate,
					TargetCadence:         v.TargetCadence,
					SortOrder:             v.SortOrder,
				},
				Remaining: rem,
			}
		}

		plan := PlanWorkspaceRollover(inputs)
		nextProjected := make(map[uuid.UUID]decimal.Decimal, len(plan.Results))
		for _, res := range plan.Results {
			nextProjected[res.CategoryID] = res.CarryoverAmount
		}
		projected = nextProjected
		m = nextM
	}
	return projected, nil
}

// ComputeMonthDetails returns the full month summary, per-category breakdown, and planned incomes.
func ComputeMonthDetails(ctx context.Context, tx pgx.Tx, yearMonth time.Time) (*MonthBudgetDetails, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	ym := NormalizeYearMonth(yearMonth)

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

	// 1b. Fetch active rollover adjustments targeting this month
	var rolloverAdjustment decimal.Decimal
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM monthly_rollover_adjustments
		WHERE workspace_id = $1
		  AND target_month = $2::date
		  AND is_active = TRUE
	`, wsID, ym).Scan(&rolloverAdjustment); err != nil {
		return nil, fmt.Errorf("failed to compute rollover adjustments: %w", err)
	}

	// 2. Fetch categories with their baseline, carryover, net reallocations, and net cleared/pending spend
	categories, err := loadMonthCategoryViews(ctx, tx, wsID, ym)
	if err != nil {
		return nil, err
	}

	currentYM := NormalizeYearMonth(time.Now().UTC())
	if ym.After(currentYM) && !ym.After(currentYM.AddDate(1, 0, 0)) {
		projected, projErr := projectFutureCarryover(ctx, tx, wsID, currentYM, ym)
		if projErr != nil {
			return nil, projErr
		}
		for i := range categories {
			if categories[i].CarryoverBalance.IsZero() {
				if p, ok := projected[categories[i].ID]; ok && !p.IsZero() {
					categories[i].CarryoverBalance = p.Round(2)
				}
			}
		}
	}

	summaryItems := make([]categoryMonthAmounts, 0, len(categories))
	for i := range categories {
		v := &categories[i]
		v.Remaining = v.CarryoverBalance.Add(v.EffectiveTarget).Sub(v.SpentCleared.Add(v.SpentPending)).Round(2)

		if v.TargetAmount != nil && v.TargetDate != nil {
			pacing := CalculateSinkingFundPace(PacingInput{
				TargetAmount:   *v.TargetAmount,
				CurrentBalance: v.Remaining,
				TargetDate:     *v.TargetDate,
				CurrentDate:    ym,
			})
			v.Pacing = &pacing
		}

		summaryItems = append(summaryItems, categoryMonthAmounts{
			CarryoverBalance: v.CarryoverBalance,
			EffectiveTarget:  v.EffectiveTarget,
			SpentCleared:     v.SpentCleared,
			SpentPending:     v.SpentPending,
		})
	}

	pendingStatus := CCInterlockPending
	pendingInterlocks, err := NewCCDebtInterlockService().ListInterlocks(ctx, tx, &ym, &pendingStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending credit card interlocks: %w", err)
	}

	summary := aggregateMonthSummary(ym, totalIncome, rolloverAdjustment, summaryItems)
	return &MonthBudgetDetails{
		Summary:           *summary,
		Categories:        categories,
		Incomes:           incomes,
		PendingInterlocks: pendingInterlocks,
	}, nil
}
