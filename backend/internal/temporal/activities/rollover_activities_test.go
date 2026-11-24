package activities

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openRolloverTestDB(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		t.Skipf("skipping DB integration test (admin pool error): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping DB integration test (postgres unreachable): %v", err)
	}

	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping DB integration test (app pool error): %v", err)
	}

	wsID := uuid.New()
	userID := uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Rollover Activity Test Workspace')", wsID); err != nil {
		adminPool.Close()
		appPool.Close()
		t.Fatalf("failed to create test workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		adminPool.Close()
		appPool.Close()
		t.Fatalf("failed to create test workspace member: %v", err)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
		adminPool.Close()
		appPool.Close()
	})

	return adminPool, appPool, wsID, userID
}

func TestRolloverActivitiesIntegration(t *testing.T) {
	adminPool, appPool, wsID, userID := openRolloverTestDB(t)
	ctx := context.Background()
	acts := &RolloverActivities{Pool: adminPool}

	sourceMonth := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	targetMonth := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	// Verify ListWorkspacesForRolloverActivity includes our workspace.
	wsList, err := acts.ListWorkspacesForRolloverActivity(ctx)
	if err != nil {
		t.Fatalf("ListWorkspacesForRolloverActivity error: %v", err)
	}
	foundWS := false
	for _, id := range wsList {
		if id == wsID {
			foundWS = true
			break
		}
	}
	if !foundWS {
		t.Fatalf("expected workspace %s in ListWorkspacesForRolloverActivity result", wsID)
	}

	// Seed accounts, categories across all rollover modes, planned income, and transactions in sourceMonth.
	var checkingID, cardID uuid.UUID
	var sinkingCatID, sweepTargetCatID, sweepToCatSrcID, sweepToPoolCatID, targetCapCatID, cashOverspendCatID, ccOverspendCatID uuid.UUID

	err = db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, 'Checking', 'checking', 3000.00, TRUE)
			RETURNING id
		`, wsID).Scan(&checkingID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget, card_mode, initial_debt_baseline)
			VALUES ($1, 'Sapphire Card', 'credit_card', -145.00, TRUE, 'PAY_IN_FULL', 0.00)
			RETURNING id
		`, wsID).Scan(&cardID); err != nil {
			return err
		}

		// Planned income for M+1 so ComputeMonthDetails(targetMonth) has a clean ReadyToAssign base.
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_planned_incomes (workspace_id, year_month, name, amount)
			VALUES ($1, $2::date, 'December Salary', 5000.00)
		`, wsID, targetMonth); err != nil {
			return err
		}

		// 1. ROLLOVER_BALANCE sinking fund: planned 500, spent 50 -> remaining 450 -> carryover 450, next planned 500.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Auto Maintenance', 'Sinking Funds', 500.00, 'ROLLOVER_BALANCE', 1)
			RETURNING id
		`, wsID).Scan(&sinkingCatID); err != nil {
			return err
		}

		// 2. Sweep target category (ROLLOVER_BALANCE): planned 100, spent 100 -> remaining 0 + 50 inbound sweep = 50 carryover.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Emergency Savings', 'Savings', 100.00, 'ROLLOVER_BALANCE', 2)
			RETURNING id
		`, wsID).Scan(&sweepTargetCatID); err != nil {
			return err
		}

		// 3. SWEEP to target category: planned 200, spent 150 -> remaining 50 swept to Emergency Savings.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sweep_target_category_id, sort_order)
			VALUES ($1, 'Groceries', 'Everyday', 200.00, 'SWEEP', $2, 3)
			RETURNING id
		`, wsID, sweepTargetCatID).Scan(&sweepToCatSrcID); err != nil {
			return err
		}

		// 4. SWEEP to pool: planned 100, spent 70 -> remaining 30 swept to Ready to Assign pool.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Utilities', 'Bills', 100.00, 'SWEEP', 4)
			RETURNING id
		`, wsID).Scan(&sweepToPoolCatID); err != nil {
			return err
		}

		// 5. TARGET_CAP: monthly_budget 1000, target_amount 1000, spent 400 -> remaining 600 -> carryover 600, next planned 400.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, target_amount, sort_order)
			VALUES ($1, 'Emergency Buffer', 'Buffers', 1000.00, 'TARGET_CAP', 1000.00, 5)
			RETURNING id
		`, wsID).Scan(&targetCapCatID); err != nil {
			return err
		}

		// 6. Cash overspend category: planned 100, cash spent 120 -> remaining -20 -> CASH_DEFICIT_DEDUCTION -20.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Transit', 'Everyday', 100.00, 'SWEEP', 6)
			RETURNING id
		`, wsID).Scan(&cashOverspendCatID); err != nil {
			return err
		}

		// 7. Credit card overspend category: planned 100, CC spent 145 -> remaining -45 -> PENDING interlock + CC_DEFICIT_DEDUCTION -45.
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, rollover_mode, sort_order)
			VALUES ($1, 'Dining Out', 'Fun', 100.00, 'SWEEP', 7)
			RETURNING id
		`, wsID).Scan(&ccOverspendCatID); err != nil {
			return err
		}

		// Freeze source month baseline for Dining Out at 100.00 (different from category default if mutated later)
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned, carryover_balance)
			VALUES ($1, $2, $3::date, 100.00, 0.00)
		`, wsID, ccOverspendCatID, sourceMonth); err != nil {
			return err
		}

		// Insert sourceMonth transactions
		txRows := []struct {
			acctID uuid.UUID
			catID  uuid.UUID
			amt    string
			status string
		}{
			{checkingID, sinkingCatID, "-50.00", "POSTED"},
			{checkingID, sweepTargetCatID, "-100.00", "POSTED"},
			{checkingID, sweepToCatSrcID, "-150.00", "POSTED"},
			{checkingID, sweepToPoolCatID, "-70.00", "POSTED"},
			{checkingID, targetCapCatID, "-400.00", "POSTED"},
			{checkingID, cashOverspendCatID, "-120.00", "POSTED"},
			{cardID, ccOverspendCatID, "-145.00", "POSTED"},
		}
		for _, r := range txRows {
			if _, err := tx.Exec(ctx, `
				INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
				VALUES ($1, $2, $3, 'MANUAL_OFFLINE', $4, $5, 'Test Spend', '2026-11-15', $6)
			`, wsID, r.acctID, r.catID, r.status, r.amt, r.status == "POSTED"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to seed rollover test data: %v", err)
	}

	// Execute rollover activity for sourceMonth (2026-11).
	res, err := acts.ExecuteWorkspaceRolloverActivity(ctx, ExecuteWorkspaceRolloverInput{
		WorkspaceID: wsID,
		SourceMonth: time.Date(2026, 11, 28, 23, 59, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ExecuteWorkspaceRolloverActivity error: %v", err)
	}
	if res.AlreadyExecuted || res.Skipped {
		t.Fatalf("expected fresh execution, got alreadyExecuted=%v skipped=%v", res.AlreadyExecuted, res.Skipped)
	}
	if res.RunID == uuid.Nil {
		t.Fatalf("expected non-nil RunID")
	}
	if res.CategoriesProcessed != 7 {
		t.Fatalf("CategoriesProcessed = %d, want 7", res.CategoriesProcessed)
	}
	if res.SweepTransfersRecorded != 1 {
		t.Fatalf("SweepTransfersRecorded = %d, want 1", res.SweepTransfersRecorded)
	}
	if res.InterlocksCreated != 1 {
		t.Fatalf("InterlocksCreated = %d, want 1", res.InterlocksCreated)
	}
	if !res.RequiresNotification {
		t.Fatalf("expected RequiresNotification = true")
	}
	if res.TotalRolledOver.StringFixed(2) != "1050.00" {
		t.Fatalf("TotalRolledOver = %s, want 1050.00", res.TotalRolledOver.StringFixed(2))
	}
	if res.TotalSweptToCategories.StringFixed(2) != "50.00" {
		t.Fatalf("TotalSweptToCategories = %s, want 50.00", res.TotalSweptToCategories.StringFixed(2))
	}
	if res.TotalSweptToPool.StringFixed(2) != "30.00" {
		t.Fatalf("TotalSweptToPool = %s, want 30.00", res.TotalSweptToPool.StringFixed(2))
	}
	if res.TotalCashDeficit.StringFixed(2) != "20.00" {
		t.Fatalf("TotalCashDeficit = %s, want 20.00", res.TotalCashDeficit.StringFixed(2))
	}
	if res.TotalCCDeficit.StringFixed(2) != "45.00" {
		t.Fatalf("TotalCCDeficit = %s, want 45.00", res.TotalCCDeficit.StringFixed(2))
	}

	// Verify budget.ComputeMonthDetails(targetMonth) reflects the frozen baselines, carryovers,
	// adjustments (+30 - 20 - 45 = -35), and pending interlock without double-counting sweeps.
	var decDetails *budget.MonthBudgetDetails
	err = db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		var txErr error
		decDetails, txErr = budget.ComputeMonthDetails(ctx, tx, targetMonth)
		return txErr
	})
	if err != nil {
		t.Fatalf("ComputeMonthDetails(targetMonth) error: %v", err)
	}

	// Expected M+1 planned totals:
	// Auto Maintenance: 500 (carryover 450)
	// Emergency Savings: 100 (carryover 50)
	// Groceries: 200 (carryover 0)
	// Utilities: 100 (carryover 0)
	// Emergency Buffer: 400 (carryover 600)
	// Transit: 100 (carryover 0)
	// Dining Out: 100 (carryover 0)
	// TotalAllocated = 1500.00
	// RolloverAdjustment = +30.00 - 20.00 - 45.00 = -35.00
	// ReadyToAssign = 5000.00 + (-35.00) - 1500.00 = 3465.00
	if decDetails.Summary.TotalAllocated.StringFixed(2) != "1500.00" {
		t.Fatalf("December TotalAllocated = %s, want 1500.00", decDetails.Summary.TotalAllocated.StringFixed(2))
	}
	if decDetails.Summary.RolloverAdjustment.StringFixed(2) != "-35.00" {
		t.Fatalf("December RolloverAdjustment = %s, want -35.00", decDetails.Summary.RolloverAdjustment.StringFixed(2))
	}
	if decDetails.Summary.ReadyToAssign.StringFixed(2) != "3465.00" {
		t.Fatalf("December ReadyToAssign = %s, want 3465.00", decDetails.Summary.ReadyToAssign.StringFixed(2))
	}
	if len(decDetails.PendingInterlocks) != 1 || decDetails.PendingInterlocks[0].DeficitAmount.StringFixed(2) != "45.00" {
		t.Fatalf("expected 1 pending interlock of 45.00, got %+v", decDetails.PendingInterlocks)
	}

	byCat := make(map[uuid.UUID]budget.CategoryBudgetView, len(decDetails.Categories))
	for _, c := range decDetails.Categories {
		byCat[c.ID] = c
	}
	if got := byCat[sinkingCatID]; got.CarryoverBalance.StringFixed(2) != "450.00" || got.OriginalPlanned.StringFixed(2) != "500.00" {
		t.Fatalf("sinking fund M+1 = %+v, want carryover 450.00 planned 500.00", got)
	}
	if got := byCat[sweepTargetCatID]; got.CarryoverBalance.StringFixed(2) != "50.00" || got.OriginalPlanned.StringFixed(2) != "100.00" {
		t.Fatalf("sweep target M+1 = %+v, want carryover 50.00 planned 100.00", got)
	}
	if got := byCat[sweepToCatSrcID]; got.CarryoverBalance.StringFixed(2) != "0.00" || got.EffectiveTarget.StringFixed(2) != "200.00" {
		t.Fatalf("sweep source M+1 = %+v, want carryover 0.00 effectiveTarget 200.00", got)
	}
	if got := byCat[targetCapCatID]; got.CarryoverBalance.StringFixed(2) != "600.00" || got.OriginalPlanned.StringFixed(2) != "400.00" {
		t.Fatalf("target cap M+1 = %+v, want carryover 600.00 planned 400.00", got)
	}

	// Verify budget_reallocations audit row was recorded under sourceMonth.
	var reallocCount int
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM budget_reallocations
		WHERE workspace_id = $1 AND year_month = $2::date AND from_category_id = $3 AND to_category_id = $4 AND amount = 50.00
	`, wsID, sourceMonth, sweepToCatSrcID, sweepTargetCatID).Scan(&reallocCount); err != nil {
		t.Fatalf("failed to query budget_reallocations: %v", err)
	}
	if reallocCount != 1 {
		t.Fatalf("expected 1 sweep audit row in budget_reallocations for sourceMonth, got %d", reallocCount)
	}

	// Verify idempotency: second execution returns AlreadyExecuted=true and creates no duplicates.
	res2, err := acts.ExecuteWorkspaceRolloverActivity(ctx, ExecuteWorkspaceRolloverInput{
		WorkspaceID: wsID,
		SourceMonth: sourceMonth,
	})
	if err != nil {
		t.Fatalf("second ExecuteWorkspaceRolloverActivity error: %v", err)
	}
	if !res2.AlreadyExecuted {
		t.Fatalf("expected AlreadyExecuted = true on second run")
	}
	if res2.RunID != res.RunID {
		t.Fatalf("second RunID = %s, want %s", res2.RunID, res.RunID)
	}

	var adjCount int
	if err := adminPool.QueryRow(ctx, `SELECT COUNT(*) FROM monthly_rollover_adjustments WHERE workspace_id = $1`, wsID).Scan(&adjCount); err != nil {
		t.Fatalf("failed to count rollover adjustments: %v", err)
	}
	if adjCount != 3 {
		t.Fatalf("expected exactly 3 rollover adjustments after idempotent rerun, got %d", adjCount)
	}

	// Verify non-existent workspace returns Skipped=true without error.
	missingRes, err := acts.ExecuteWorkspaceRolloverActivity(ctx, ExecuteWorkspaceRolloverInput{
		WorkspaceID: uuid.New(),
		SourceMonth: sourceMonth,
	})
	if err != nil {
		t.Fatalf("missing workspace rollover returned error: %v", err)
	}
	if !missingRes.Skipped {
		t.Fatalf("expected Skipped = true for non-existent workspace")
	}
}
