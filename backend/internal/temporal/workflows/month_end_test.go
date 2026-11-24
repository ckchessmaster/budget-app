package workflows

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

func TestMonthEndRolloverWorkflow_Unit(t *testing.T) {
	t.Run("scheduled run infers previous month from workflow start time with SyncScope", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.SetStartTime(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

		scope := temporalinternal.SyncScope{
			WorkspaceID: uuid.New(),
			UserID:      uuid.New(),
		}
		expectedSource := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		expectedTarget := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

		var acts *activities.RolloverActivities
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			SyncScope:   scope,
			SourceMonth: expectedSource,
		}).Return(&activities.WorkspaceRolloverResult{
			WorkspaceID:         scope.WorkspaceID,
			SourceMonth:         expectedSource,
			TargetMonth:         expectedTarget,
			CategoriesProcessed: 3,
		}, nil).Once()

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{
			SyncScope: scope,
		})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("workflow returned unexpected error: %v", err)
		}

		var res activities.WorkspaceRolloverResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("failed to decode workflow result: %v", err)
		}
		if !res.SourceMonth.Equal(expectedSource) || !res.TargetMonth.Equal(expectedTarget) {
			t.Fatalf("months = (%v, %v), want (%v, %v)", res.SourceMonth, res.TargetMonth, expectedSource, expectedTarget)
		}
		if res.CategoriesProcessed != 3 {
			t.Fatalf("CategoriesProcessed = %d, want 3", res.CategoriesProcessed)
		}
		env.AssertExpectations(t)
	})

	t.Run("explicit source month is normalized to first of month", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()

		scope := temporalinternal.SyncScope{
			WorkspaceID: uuid.New(),
			UserID:      uuid.New(),
		}
		source := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		target := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

		var acts *activities.RolloverActivities
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			SyncScope:   scope,
			SourceMonth: source,
		}).Return(&activities.WorkspaceRolloverResult{
			WorkspaceID:         scope.WorkspaceID,
			SourceMonth:         source,
			TargetMonth:         target,
			CategoriesProcessed: 2,
		}, nil).Once()

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{
			SyncScope:   scope,
			SourceMonth: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var res activities.WorkspaceRolloverResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("failed to decode result: %v", err)
		}
		if res.CategoriesProcessed != 2 {
			t.Fatalf("CategoriesProcessed = %d, want 2", res.CategoriesProcessed)
		}
		env.AssertExpectations(t)
	})

	t.Run("missing SyncScope fails without calling activity", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{
			SyncScope: temporalinternal.SyncScope{WorkspaceID: uuid.New()},
		})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err == nil {
			t.Fatalf("expected error when UserID is missing")
		}
		env.AssertNotCalled(t, "ExecuteWorkspaceRolloverActivity", mock.Anything, mock.Anything)
	})

	t.Run("activity failure propagates workflow error after retries", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.SetStartTime(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

		scope := temporalinternal.SyncScope{
			WorkspaceID: uuid.New(),
			UserID:      uuid.New(),
		}
		source := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

		var acts *activities.RolloverActivities
		env.OnActivity(acts.ExecuteWorkspaceRolloverActivity, mock.Anything, activities.ExecuteWorkspaceRolloverInput{
			SyncScope:   scope,
			SourceMonth: source,
		}).Return(nil, errors.New("db lock timeout")).Times(3)

		env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{
			SyncScope: scope,
		})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("expected workflow to complete")
		}
		if err := env.GetWorkflowError(); err == nil {
			t.Fatalf("expected workflow error when activity fails")
		}
		env.AssertExpectations(t)
	})
}

func TestMonthEndRolloverWorkflow_TemporalSimulationWithDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	appURL := os.Getenv("TEST_DATABASE_URL")
	if appURL == "" {
		appURL = adminURL
	}

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("skipping Temporal DB simulation (admin pool error): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping Temporal DB simulation (postgres unreachable): %v", err)
	}
	defer adminPool.Close()

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Skipf("skipping Temporal DB simulation (app pool error): %v", err)
	}
	defer appPool.Close()

	wsID := uuid.New()
	userID := uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Temporal Simulation WS')", wsID); err != nil {
		t.Fatalf("failed to create simulation workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to create simulation workspace member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	var checkingID, cardID uuid.UUID
	var sinkingCatID, sweepTargetID, sweepSrcID, ccCatID uuid.UUID

	err = db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, 'Checking', 'checking', 2000.00, TRUE)
			RETURNING id
		`, wsID).Scan(&checkingID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget, card_mode)
			VALUES ($1, 'Visa', 'credit_card', -45.00, TRUE, 'PAY_IN_FULL')
			RETURNING id
		`, wsID).Scan(&cardID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Car Repair', 'Sinking', 500.00, 'ROLLOVER_BALANCE', 1)
			RETURNING id
		`, wsID).Scan(&sinkingCatID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Savings Vault', 'Savings', 100.00, 'ROLLOVER_BALANCE', 2)
			RETURNING id
		`, wsID).Scan(&sweepTargetID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sweep_target_category_id, sort_order)
			VALUES ($1, 'Coffee', 'Fun', 100.00, 'SWEEP', $2, 3)
			RETURNING id
		`, wsID, sweepTargetID).Scan(&sweepSrcID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Dining', 'Fun', 100.00, 'SWEEP', 4)
			RETURNING id
		`, wsID).Scan(&ccCatID); err != nil {
			return err
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
			if _, err := tx.Exec(ctx, `
				INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
				VALUES ($1, $2, $3, 'MANUAL_OFFLINE', 'POSTED', $4, 'Sim Merchant', '2026-11-20', TRUE)
			`, wsID, s.acct, s.cat, s.amt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to seed simulation data: %v", err)
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

	realActivities := &activities.RolloverActivities{Pool: appPool}
	env.RegisterActivity(realActivities)

	env.ExecuteWorkflow(MonthEndRolloverWorkflow, MonthEndRolloverInput{
		SyncScope: temporalinternal.SyncScope{
			WorkspaceID: wsID,
			UserID:      userID,
		},
	})
	if !env.IsWorkflowCompleted() {
		t.Fatalf("expected workflow to complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("Temporal simulation failed: %v", err)
	}

	var wfRes activities.WorkspaceRolloverResult
	if err := env.GetWorkflowResult(&wfRes); err != nil {
		t.Fatalf("failed to decode workflow result: %v", err)
	}
	if wfRes.WorkspaceID != wsID || wfRes.AlreadyExecuted || wfRes.Skipped {
		t.Fatalf("unexpected workflow summary: %+v", wfRes)
	}

	decMonth := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	novMonth := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	// Verify new month baselines exist in monthly_category_budgets for 2026-12-01.
	var decBaselineCount int
	if err := adminPool.QueryRow(ctx, `
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
	if err := adminPool.QueryRow(ctx, `
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
	if err := adminPool.QueryRow(ctx, `
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
	if err := adminPool.QueryRow(ctx, `
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
	if err := adminPool.QueryRow(ctx, `
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
	if err := adminPool.QueryRow(ctx, `
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
