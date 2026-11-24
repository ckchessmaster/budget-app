package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.temporal.io/sdk/temporal"
)

// RolloverActivities implements Temporal activities for executing month-end budget rollovers.
type RolloverActivities struct {
	Pool *pgxpool.Pool
}

// ExecuteWorkspaceRolloverInput identifies the workspace and source month (M) to roll over into M+1.
type ExecuteWorkspaceRolloverInput struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	SourceMonth time.Time `json:"source_month"`
}

// WorkspaceRolloverResult summarizes the persisted month-end rollover outcome for a single workspace.
type WorkspaceRolloverResult struct {
	WorkspaceID            uuid.UUID       `json:"workspace_id"`
	SourceMonth            time.Time       `json:"source_month"`
	TargetMonth            time.Time       `json:"target_month"`
	RunID                  uuid.UUID       `json:"run_id,omitempty"`
	AlreadyExecuted        bool            `json:"already_executed"`
	Skipped                bool            `json:"skipped"`
	CategoriesProcessed    int             `json:"categories_processed"`
	SweepTransfersRecorded int             `json:"sweep_transfers_recorded"`
	InterlocksCreated      int             `json:"interlocks_created"`
	TotalRolledOver        decimal.Decimal `json:"total_rolled_over"`
	TotalSweptToCategories decimal.Decimal `json:"total_swept_to_categories"`
	TotalSweptToPool       decimal.Decimal `json:"total_swept_to_pool"`
	TotalCashDeficit       decimal.Decimal `json:"total_cash_deficit"`
	TotalCCDeficit         decimal.Decimal `json:"total_cc_deficit"`
	RequiresNotification   bool            `json:"requires_notification"`
}

type rolloverCategoryState struct {
	Category     budget.Category
	CategoryName string
	Remaining    decimal.Decimal
}

// ListWorkspacesForRolloverActivity lists all workspace IDs eligible for month-end rollover.
func (a *RolloverActivities) ListWorkspacesForRolloverActivity(ctx context.Context) ([]uuid.UUID, error) {
	if a.Pool == nil {
		return nil, temporal.NewNonRetryableApplicationError("database pool is not configured", "NilPoolError", nil)
	}

	rows, err := a.Pool.Query(ctx, `
		SELECT id
		FROM workspaces
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspaces for rollover: %w", err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan workspace id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workspace ids: %w", err)
	}
	return ids, nil
}

// ExecuteWorkspaceRolloverActivity evaluates and persists a workspace's month-end rollover from M to M+1
// inside a single idempotent PostgreSQL transaction.
func (a *RolloverActivities) ExecuteWorkspaceRolloverActivity(ctx context.Context, input ExecuteWorkspaceRolloverInput) (*WorkspaceRolloverResult, error) {
	if a.Pool == nil {
		return nil, temporal.NewNonRetryableApplicationError("database pool is not configured", "NilPoolError", nil)
	}
	if input.WorkspaceID == uuid.Nil {
		return nil, temporal.NewNonRetryableApplicationError("workspace_id is required", "InvalidRolloverInput", nil)
	}
	if input.SourceMonth.IsZero() {
		return nil, temporal.NewNonRetryableApplicationError("source_month is required", "InvalidRolloverInput", nil)
	}

	sourceMonth := budget.NormalizeYearMonth(input.SourceMonth)
	targetMonth := sourceMonth.AddDate(0, 1, 0)

	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin rollover transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Lock workspace row so concurrent runs serialize and missing workspaces skip cleanly.
	var lockedWS uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM workspaces
		WHERE id = $1
		FOR UPDATE
	`, input.WorkspaceID).Scan(&lockedWS); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &WorkspaceRolloverResult{
				WorkspaceID: input.WorkspaceID,
				SourceMonth: sourceMonth,
				TargetMonth: targetMonth,
				Skipped:     true,
			}, nil
		}
		return nil, fmt.Errorf("failed to lock workspace %s: %w", input.WorkspaceID, err)
	}

	// 2. Idempotency check against month_end_rollover_runs.
	var existing WorkspaceRolloverResult
	err = tx.QueryRow(ctx, `
		SELECT id, total_rolled_over, total_swept_to_categories, total_swept_to_pool, total_cash_deficit, total_cc_deficit
		FROM month_end_rollover_runs
		WHERE workspace_id = $1 AND source_month = $2::date
	`, input.WorkspaceID, sourceMonth).Scan(
		&existing.RunID,
		&existing.TotalRolledOver,
		&existing.TotalSweptToCategories,
		&existing.TotalSweptToPool,
		&existing.TotalCashDeficit,
		&existing.TotalCCDeficit,
	)
	if err == nil {
		existing.WorkspaceID = input.WorkspaceID
		existing.SourceMonth = sourceMonth
		existing.TargetMonth = targetMonth
		existing.AlreadyExecuted = true
		existing.TotalRolledOver = existing.TotalRolledOver.Round(2)
		existing.TotalSweptToCategories = existing.TotalSweptToCategories.Round(2)
		existing.TotalSweptToPool = existing.TotalSweptToPool.Round(2)
		existing.TotalCashDeficit = existing.TotalCashDeficit.Round(2)
		existing.TotalCCDeficit = existing.TotalCCDeficit.Round(2)
		existing.RequiresNotification = existing.TotalCCDeficit.GreaterThan(decimal.Zero)
		return &existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to check rollover run idempotency: %w", err)
	}

	// 3. Load source month (M) category baselines, reallocations, and spend.
	catStates, err := loadSourceMonthCategoryStates(ctx, tx, input.WorkspaceID, sourceMonth, targetMonth)
	if err != nil {
		return nil, err
	}

	// 4. Load source month (M) credit card spend per category per card.
	ccSpendByCat, err := loadWorkspaceCategoryCCSpend(ctx, tx, input.WorkspaceID, sourceMonth, targetMonth)
	if err != nil {
		return nil, err
	}

	rolloverInputs := make([]budget.CategoryRolloverInput, len(catStates))
	deficitInputs := make([]budget.CategoryDeficitInput, len(catStates))
	for i, st := range catStates {
		rolloverInputs[i] = budget.CategoryRolloverInput{
			Category:  st.Category,
			Remaining: st.Remaining,
		}
		deficitInputs[i] = budget.CategoryDeficitInput{
			CategoryID:    st.Category.ID,
			CategoryName:  st.CategoryName,
			Remaining:     st.Remaining,
			CCSpendByCard: ccSpendByCat[st.Category.ID],
		}
	}

	// 5. Evaluate pure rollover and credit card interlock plans.
	rolloverPlan := budget.PlanWorkspaceRollover(rolloverInputs)
	interlockPlan := budget.EvaluateCCDebtInterlock(sourceMonth, deficitInputs)

	// 6. Freeze new monthly baselines in monthly_category_budgets for targetMonth (M+1).
	for _, res := range rolloverPlan.Results {
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned, carryover_balance)
			VALUES ($1, $2, $3::date, $4, $5)
			ON CONFLICT (workspace_id, category_id, year_month)
			DO UPDATE SET original_planned = EXCLUDED.original_planned,
			              carryover_balance = EXCLUDED.carryover_balance
		`, input.WorkspaceID, res.CategoryID, targetMonth, res.NextMonthAssigned.Round(2), res.CarryoverAmount.Round(2)); err != nil {
			return nil, fmt.Errorf("failed to freeze target month baseline for category %s: %w", res.CategoryID, err)
		}
	}

	// 7. Audit category-to-category sweeps in budget_reallocations under sourceMonth (M)
	// so ComputeMonthDetails(M+1) does not double-count them alongside carryover_balance.
	sweepReason := fmt.Sprintf("Month-end rollover sweep (%s -> %s)", sourceMonth.Format("2006-01"), targetMonth.Format("2006-01"))
	sweepCount := 0
	for _, st := range rolloverPlan.SweepTransfers {
		amt := st.Amount.Round(2)
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO budget_reallocations (workspace_id, year_month, from_category_id, to_category_id, amount, reason)
			VALUES ($1, $2::date, $3, $4, $5, $6)
		`, input.WorkspaceID, sourceMonth, st.FromCategoryID, st.ToCategoryID, amt, sweepReason); err != nil {
			return nil, fmt.Errorf("failed to audit rollover sweep transfer: %w", err)
		}
		sweepCount++
	}

	// 8. Insert SWEEP_TO_POOL and CASH_DEFICIT_DEDUCTION rollover adjustments targeting M+1.
	for _, res := range rolloverPlan.Results {
		amt := res.SweptToPool.Round(2)
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}
		catID := res.CategoryID
		reason := fmt.Sprintf("Month-end sweep to Ready to Assign from %s", sourceMonth.Format("2006-01"))
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_rollover_adjustments
				(workspace_id, source_month, target_month, category_id, interlock_id, adjustment_type, amount, is_active, reason)
			VALUES ($1, $2::date, $3::date, $4, NULL, 'SWEEP_TO_POOL', $5, TRUE, $6)
		`, input.WorkspaceID, sourceMonth, targetMonth, catID, amt, reason); err != nil {
			return nil, fmt.Errorf("failed to insert SWEEP_TO_POOL adjustment: %w", err)
		}
	}

	for _, det := range interlockPlan.CategoryDetails {
		cashDeficit := det.CashDeficit.Round(2)
		if !cashDeficit.GreaterThan(decimal.Zero) {
			continue
		}
		catID := det.CategoryID
		reason := fmt.Sprintf("Cash overspend deficit from %s (%s)", sourceMonth.Format("2006-01"), det.CategoryName)
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_rollover_adjustments
				(workspace_id, source_month, target_month, category_id, interlock_id, adjustment_type, amount, is_active, reason)
			VALUES ($1, $2::date, $3::date, $4, NULL, 'CASH_DEFICIT_DEDUCTION', $5, TRUE, $6)
		`, input.WorkspaceID, sourceMonth, targetMonth, catID, cashDeficit.Neg().Round(2), reason); err != nil {
			return nil, fmt.Errorf("failed to insert CASH_DEFICIT_DEDUCTION adjustment: %w", err)
		}
	}

	// 9. Insert PENDING credit_card_debt_interlocks and linked CC_DEFICIT_DEDUCTION rows targeting M+1.
	interlocksCreated := 0
	for _, item := range interlockPlan.AccountInterlocks {
		amt := item.DeficitAmount.Round(2)
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}
		var acctID *uuid.UUID
		if item.AccountID != nil && *item.AccountID != uuid.Nil {
			cp := *item.AccountID
			acctID = &cp
		}

		var ilID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO credit_card_debt_interlocks
				(workspace_id, account_id, source_month, target_month, deficit_amount, status)
			VALUES ($1, $2, $3::date, $4::date, $5, 'PENDING')
			ON CONFLICT (workspace_id, account_id, source_month) DO NOTHING
			RETURNING id
		`, input.WorkspaceID, acctID, sourceMonth, targetMonth, amt).Scan(&ilID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to insert credit card debt interlock: %w", err)
		}

		reason := fmt.Sprintf("Uncovered credit card overspend from %s", sourceMonth.Format("2006-01"))
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_rollover_adjustments
				(workspace_id, source_month, target_month, category_id, interlock_id, adjustment_type, amount, is_active, reason)
			VALUES ($1, $2::date, $3::date, NULL, $4, 'CC_DEFICIT_DEDUCTION', $5, TRUE, $6)
		`, input.WorkspaceID, sourceMonth, targetMonth, ilID, amt.Neg().Round(2), reason); err != nil {
			return nil, fmt.Errorf("failed to insert CC_DEFICIT_DEDUCTION adjustment: %w", err)
		}
		interlocksCreated++
	}

	// 10. Record audit run in month_end_rollover_runs.
	var runID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO month_end_rollover_runs (
			workspace_id, source_month, target_month,
			total_rolled_over, total_swept_to_categories, total_swept_to_pool,
			total_cash_deficit, total_cc_deficit
		)
		VALUES ($1, $2::date, $3::date, $4, $5, $6, $7, $8)
		RETURNING id
	`,
		input.WorkspaceID,
		sourceMonth,
		targetMonth,
		rolloverPlan.TotalRolledOver.Round(2),
		rolloverPlan.TotalSweptToCategories.Round(2),
		rolloverPlan.TotalSweptToPool.Round(2),
		interlockPlan.TotalCashDeficit.Round(2),
		interlockPlan.TotalCCDeficit.Round(2),
	).Scan(&runID); err != nil {
		return nil, fmt.Errorf("failed to insert month_end_rollover_runs record: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit rollover transaction: %w", err)
	}

	return &WorkspaceRolloverResult{
		WorkspaceID:            input.WorkspaceID,
		SourceMonth:            sourceMonth,
		TargetMonth:            targetMonth,
		RunID:                  runID,
		AlreadyExecuted:        false,
		Skipped:                false,
		CategoriesProcessed:    len(rolloverPlan.Results),
		SweepTransfersRecorded: sweepCount,
		InterlocksCreated:      interlocksCreated,
		TotalRolledOver:        rolloverPlan.TotalRolledOver.Round(2),
		TotalSweptToCategories: rolloverPlan.TotalSweptToCategories.Round(2),
		TotalSweptToPool:       rolloverPlan.TotalSweptToPool.Round(2),
		TotalCashDeficit:       interlockPlan.TotalCashDeficit.Round(2),
		TotalCCDeficit:         interlockPlan.TotalCCDeficit.Round(2),
		RequiresNotification:   interlockPlan.RequiresNotification,
	}, nil
}

func loadSourceMonthCategoryStates(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID uuid.UUID,
	sourceMonth time.Time,
	targetMonth time.Time,
) ([]rolloverCategoryState, error) {
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
			       COALESCE(SUM(CASE WHEN t.status = 'POSTED' AND t.amount < 0 THEN -t.amount ELSE 0 END), 0) AS spent_cleared,
			       COALESCE(SUM(CASE WHEN t.status = 'PENDING' AND t.amount < 0 THEN -t.amount ELSE 0 END), 0) AS spent_pending
			FROM transactions t
			INNER JOIN accounts a ON a.id = t.account_id
			WHERE t.workspace_id = $3
			  AND a.workspace_id = $3
			  AND t.category_id IS NOT NULL
			  AND t.is_transfer = FALSE
			  AND a.is_on_budget = TRUE
			  AND t.transaction_date >= $1::date
			  AND t.transaction_date < $2::date
			GROUP BY t.category_id
		)
		SELECT c.id,
		       c.workspace_id,
		       c.name,
		       c.group_name,
		       c.rollover_mode,
		       c.sweep_target_category_id,
		       c.target_amount,
		       c.target_date,
		       c.target_cadence,
		       c.sort_order,
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
		ORDER BY c.sort_order ASC, c.group_name ASC, c.name ASC, c.id ASC
	`, sourceMonth, targetMonth, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load source month categories: %w", err)
	}
	defer rows.Close()

	states := make([]rolloverCategoryState, 0)
	for rows.Next() {
		var cat budget.Category
		var rolloverMode string
		var originalPlanned, carryoverBalance, netTransfer, spentCleared, spentPending decimal.Decimal
		if err := rows.Scan(
			&cat.ID,
			&cat.WorkspaceID,
			&cat.Name,
			&cat.GroupName,
			&rolloverMode,
			&cat.SweepTargetCategoryID,
			&cat.TargetAmount,
			&cat.TargetDate,
			&cat.TargetCadence,
			&cat.SortOrder,
			&originalPlanned,
			&carryoverBalance,
			&netTransfer,
			&spentCleared,
			&spentPending,
		); err != nil {
			return nil, fmt.Errorf("failed to scan source month category state: %w", err)
		}
		cat.RolloverMode = budget.RolloverMode(rolloverMode)
		// Preserve frozen month M baseline when evaluating next month's default planned amount.
		cat.MonthlyBudget = originalPlanned.Round(2)

		effectiveTarget := originalPlanned.Add(netTransfer).Round(2)
		remaining := carryoverBalance.Add(effectiveTarget).Sub(spentCleared.Add(spentPending)).Round(2)

		states = append(states, rolloverCategoryState{
			Category:     cat,
			CategoryName: cat.Name,
			Remaining:    remaining,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating source month categories: %w", err)
	}
	return states, nil
}

func loadWorkspaceCategoryCCSpend(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID uuid.UUID,
	sourceMonth time.Time,
	targetMonth time.Time,
) (map[uuid.UUID][]budget.CCAccountSpend, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.category_id, t.account_id, COALESCE(SUM(-t.amount), 0) AS spent
		FROM transactions t
		INNER JOIN accounts a ON a.id = t.account_id
		WHERE t.workspace_id = $3
		  AND a.workspace_id = $3
		  AND t.category_id IS NOT NULL
		  AND t.is_transfer = FALSE
		  AND t.amount < 0
		  AND a.is_on_budget = TRUE
		  AND a.type = 'credit_card'
		  AND t.transaction_date >= $1::date
		  AND t.transaction_date < $2::date
		GROUP BY t.category_id, t.account_id
		ORDER BY t.category_id ASC, t.account_id ASC
	`, sourceMonth, targetMonth, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load workspace credit card spend: %w", err)
	}
	defer rows.Close()

	byCategory := make(map[uuid.UUID][]budget.CCAccountSpend)
	for rows.Next() {
		var catID, acctID uuid.UUID
		var spent decimal.Decimal
		if err := rows.Scan(&catID, &acctID, &spent); err != nil {
			return nil, fmt.Errorf("failed to scan workspace credit card spend: %w", err)
		}
		amt := spent.Round(2)
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}
		byCategory[catID] = append(byCategory[catID], budget.CCAccountSpend{
			AccountID: acctID,
			Amount:    amt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workspace credit card spend: %w", err)
	}
	return byCategory, nil
}
