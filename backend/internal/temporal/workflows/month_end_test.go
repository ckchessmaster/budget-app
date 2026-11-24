package workflows

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

func TestMonthEndRolloverWorkflow_Unit(t *testing.T) {
	t.Run("scheduled run infers previous month from workflow start time and fans out", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.SetStartTime(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

		ws1 := uuid.New()
		ws2 := uuid.New()
		expectedSource := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		expectedTarget := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

		var acts *activities.RolloverActivities
		env.OnActivity(acts.ListWorkspacesForRolloverActivity, mock.Anything).Return([]uuid.UUID{ws1, ws2}, nil).Once()
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			WorkspaceID: ws1,
			SourceMonth: expectedSource,
		}).Return(&activities.WorkspaceRolloverResult{
			WorkspaceID:         ws1,
			SourceMonth:         expectedSource,
			TargetMonth:         expectedTarget,
			CategoriesProcessed: 3,
		}, nil).Once()
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			WorkspaceID: ws2,
			SourceMonth: expectedSource,
		}).Return(&activities.WorkspaceRolloverResult{
			WorkspaceID:     ws2,
			SourceMonth:     expectedSource,
			TargetMonth:     expectedTarget,
			AlreadyExecuted: true,
		}, nil).Once()

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("workflow returned unexpected error: %v", err)
		}

		var res MonthEndRolloverResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("failed to decode workflow result: %v", err)
		}
		if !res.SourceMonth.Equal(expectedSource) || !res.TargetMonth.Equal(expectedTarget) {
			t.Fatalf("months = (%v, %v), want (%v, %v)", res.SourceMonth, res.TargetMonth, expectedSource, expectedTarget)
		}
		if res.WorkspacesProcessed != 1 || res.WorkspacesSkipped != 1 || res.WorkspacesFailed != 0 {
			t.Fatalf("counts = processed:%d skipped:%d failed:%d, want 1/1/0", res.WorkspacesProcessed, res.WorkspacesSkipped, res.WorkspacesFailed)
		}
		env.AssertExpectations(t)
	})

	t.Run("explicit workspace id skips workspace listing", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()

		wsID := uuid.New()
		source := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		target := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

		var acts *activities.RolloverActivities
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			WorkspaceID: wsID,
			SourceMonth: source,
		}).Return(&activities.WorkspaceRolloverResult{
			WorkspaceID:         wsID,
			SourceMonth:         source,
			TargetMonth:         target,
			CategoriesProcessed: 2,
		}, nil).Once()

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{
			SourceMonth: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
			WorkspaceID: &wsID,
		})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var res MonthEndRolloverResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("failed to decode result: %v", err)
		}
		if res.WorkspacesProcessed != 1 {
			t.Fatalf("WorkspacesProcessed = %d, want 1", res.WorkspacesProcessed)
		}
		env.AssertNotCalled(t, "ListWorkspacesForRolloverActivity", mock.Anything)
		env.AssertExpectations(t)
	})

	t.Run("partial failure executes remaining workspaces and returns error", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.SetStartTime(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

		wsBad := uuid.New()
		wsGood := uuid.New()
		source := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

		var acts *activities.RolloverActivities
		env.OnActivity(acts.ListWorkspacesForRolloverActivity, mock.Anything).Return([]uuid.UUID{wsBad, wsGood}, nil).Once()
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			WorkspaceID: wsBad,
			SourceMonth: source,
		}).Return(nil, errors.New("db lock timeout")).Times(3)
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			WorkspaceID: wsGood,
			SourceMonth: source,
		}).Return(&activities.WorkspaceRolloverResult{
			WorkspaceID:         wsGood,
			SourceMonth:         source,
			TargetMonth:         time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			CategoriesProcessed: 1,
		}, nil).Once()

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err == nil {
			t.Fatalf("expected workflow error when one workspace failed")
		}
		env.AssertExpectations(t)
	})
}

func TestMonthEndRolloverWorkflow_TemporalSimulationWithDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping Temporal DB simulation (pool error): %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping Temporal DB simulation (postgres unreachable): %v", err)
	}
	defer pool.Close()

	wsID := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Temporal Simulation WS')", wsID); err != nil {
		t.Fatalf("failed to create simulation workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	var checkingID, cardID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
		VALUES ($1, 'Checking', 'checking', 2000.00, TRUE)
		RETURNING id
	`, wsID).Scan(&checkingID); err != nil {
		t.Fatalf("failed to create checking account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget, card_mode)
		VALUES ($1, 'Visa', 'credit_card', -45.00, TRUE, 'PAY_IN_FULL')
		RETURNING id
	`, wsID).Scan(&cardID); err != nil {
		t.Fatalf("failed to create credit card account: %v", err)
	}

	var sinkingCatID, sweepTargetID, sweepSrcID, ccCatID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
		VALUES ($1, 'Car Repair', 'Sinking', 500.00, 'ROLLOVER_BALANCE', 1)
		RETURNING id
	`, wsID).Scan(&sinkingCatID); err != nil {
		t.Fatalf("failed to create sinking category: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
		VALUES ($1, 'Savings Vault', 'Savings', 100.00, 'ROLLOVER_BALANCE', 2)
		RETURNING id
	`, wsID).Scan(&sweepTargetID); err != nil {
		t.Fatalf("failed to create sweep target category: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sweep_target_category_id, sort_order)
		VALUES ($1, 'Coffee', 'Fun', 100.00, 'SWEEP', $2, 3)
		RETURNING id
	`, wsID, sweepTargetID).Scan(&sweepSrcID); err != nil {
		t.Fatalf("failed to create sweep source category: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
		VALUES ($1, 'Dining', 'Fun', 100.00, 'SWEEP', 4)
		RETURNING id
	`, wsID).Scan(&ccCatID); err != nil {
		t.Fatalf("failed to create CC category: %v", err)
	}

	// Spend in November 2026:
	// Car Repair: -50 -> +450 remaining (ROLLOVER_BALANCE)
	// Savings Vault: -100 -> 0 remaining
	// Coffee: -50 -> +50 remaining (SWEEP -> Savings Vault)
	// Dining: -145 on Visa -> -45 CC overspend
	seedTx := []struct {
		acct uuid.UUID
		cat  uuid.UUID
		amt  string
	}{
		{checkingID, sinkingCatID, "-50.00"},
		{checkingID, sweepTargetID, "-100.00"},
		{checkingID, sweepSrcID, "-50.00"},
		{cardID, ccCatID, "-145.00"},
	}
	for _, s := range seedTx {
		if _, err := pool.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
			VALUES ($1, $2, $3, 'MANUAL_OFFLINE', 'POSTED', $4, 'Sim Merchant', '2026-11-20', TRUE)
		`, wsID, s.acct, s.cat, s.amt); err != nil {
			t.Fatalf("failed to seed transaction: %v", err)
		}
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

	realActivities := &activities.RolloverActivities{Pool: pool}
	env.RegisterActivity(realActivities)
	// Restrict fan-out in the test DB to our isolated workspace.
	env.OnActivity(realActivities.ListWorkspacesForRolloverActivity, mock.Anything).Return([]uuid.UUID{wsID}, nil).Once()

	env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{})
	if !env.IsWorkflowCompleted() {
		t.Fatalf("expected workflow to complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("Temporal simulation failed: %v", err)
	}

	var wfRes MonthEndRolloverResult
	if err := env.GetWorkflowResult(&wfRes); err != nil {
		t.Fatalf("failed to decode workflow result: %v", err)
	}
	if wfRes.WorkspacesProcessed != 1 || len(wfRes.Results) != 1 {
		t.Fatalf("unexpected workflow summary: %+v", wfRes)
	}

	decMonth := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	novMonth := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	// Verify new month baselines exist in monthly_category_budgets for 2026-12-01.
	var decBaselineCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM monthly_category_budgets
		WHERE workspace_id = $1 AND year_month = $2::date
	`, wsID, decMonth).Scan(&decBaselineCount); err != nil {
		t.Fatalf("failed to query monthly_category_budgets: %v", err)
	}
	if decBaselineCount != 4 {
		t.Fatalf("expected 4 December baselines, got %d", decBaselineCount)
	}

	var sinkingCarryover, sinkingPlanned decimal.Decimal
	if err := pool.QueryRow(ctx, `
		SELECT carryover_balance, original_planned
		FROM monthly_category_budgets
		WHERE workspace_id = $1 AND category_id = $2 AND year_month = $3::date
	`, wsID, sinkingCatID, decMonth).Scan(&sinkingCarryover, &sinkingPlanned); err != nil {
		t.Fatalf("failed to query sinking baseline: %v", err)
	}
	if sinkingCarryover.StringFixed(2) != "450.00" || sinkingPlanned.StringFixed(2) != "500.00" {
		t.Fatalf("sinking baseline = carryover %s planned %s, want 450.00 / 500.00", sinkingCarryover, sinkingPlanned)
	}

	var sweepTargetCarryover decimal.Decimal
	if err := pool.QueryRow(ctx, `
		SELECT carryover_balance
		FROM monthly_category_budgets
		WHERE workspace_id = $1 AND category_id = $2 AND year_month = $3::date
	`, wsID, sweepTargetID, decMonth).Scan(&sweepTargetCarryover); err != nil {
		t.Fatalf("failed to query sweep target baseline: %v", err)
	}
	if sweepTargetCarryover.StringFixed(2) != "50.00" {
		t.Fatalf("sweep target carryover = %s, want 50.00", sweepTargetCarryover)
	}

	// Verify audit rows in budget_reallocations, credit_card_debt_interlocks, and month_end_rollover_runs.
	var reallocAmt decimal.Decimal
	if err := pool.QueryRow(ctx, `
		SELECT amount
		FROM budget_reallocations
		WHERE workspace_id = $1 AND year_month = $2::date AND from_category_id = $3 AND to_category_id = $4
	`, wsID, novMonth, sweepSrcID, sweepTargetID).Scan(&reallocAmt); err != nil {
		t.Fatalf("expected budget_reallocations audit entry: %v", err)
	}
	if reallocAmt.StringFixed(2) != "50.00" {
		t.Fatalf("sweep reallocation amount = %s, want 50.00", reallocAmt)
	}

	var ilDeficit decimal.Decimal
	var ilStatus string
	if err := pool.QueryRow(ctx, `
		SELECT deficit_amount, status
		FROM credit_card_debt_interlocks
		WHERE workspace_id = $1 AND source_month = $2::date AND target_month = $3::date AND account_id = $4
	`, wsID, novMonth, decMonth, cardID).Scan(&ilDeficit, &ilStatus); err != nil {
		t.Fatalf("expected credit_card_debt_interlocks row: %v", err)
	}
	if ilDeficit.StringFixed(2) != "45.00" || ilStatus != "PENDING" {
		t.Fatalf("interlock = (%s, %s), want (45.00, PENDING)", ilDeficit, ilStatus)
	}

	var runCCDeficit decimal.Decimal
	if err := pool.QueryRow(ctx, `
		SELECT total_cc_deficit
		FROM month_end_rollover_runs
		WHERE workspace_id = $1 AND source_month = $2::date AND target_month = $3::date
	`, wsID, novMonth, decMonth).Scan(&runCCDeficit); err != nil {
		t.Fatalf("expected month_end_rollover_runs row: %v", err)
	}
	if runCCDeficit.StringFixed(2) != "45.00" {
		t.Fatalf("run total_cc_deficit = %s, want 45.00", runCCDeficit)
	}
}
