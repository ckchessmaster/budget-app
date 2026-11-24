package budget

import (
	"context"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestAggregateMonthSummary(t *testing.T) {
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	t.Run("balanced zero-based budget", func(t *testing.T) {
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), decimal.Zero, []categoryMonthAmounts{
			{EffectiveTarget: decimal.NewFromInt(2500), SpentCleared: decimal.NewFromInt(2000), SpentPending: decimal.Zero},
			{EffectiveTarget: decimal.NewFromInt(1500), SpentCleared: decimal.NewFromInt(500), SpentPending: decimal.NewFromInt(100)},
		})
		if summary.ReadyToAssign.StringFixed(2) != "0.00" {
			t.Fatalf("ReadyToAssign = %s, want 0.00", summary.ReadyToAssign.StringFixed(2))
		}
		if summary.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = false")
		}
	})

	t.Run("overspend sets IsUnbalanced = true", func(t *testing.T) {
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), decimal.Zero, []categoryMonthAmounts{
			{EffectiveTarget: decimal.NewFromInt(2500), SpentCleared: decimal.NewFromInt(2550), SpentPending: decimal.Zero},
			{EffectiveTarget: decimal.NewFromInt(1500), SpentCleared: decimal.Zero, SpentPending: decimal.Zero},
		})
		if !summary.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = true on $50 overspend")
		}
	})

	t.Run("carryover balance increases available without inflating TotalAllocated", func(t *testing.T) {
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), decimal.Zero, []categoryMonthAmounts{
			{
				CarryoverBalance: decimal.NewFromInt(450),
				EffectiveTarget:  decimal.NewFromInt(200),
				SpentCleared:     decimal.NewFromInt(500),
				SpentPending:     decimal.Zero,
			},
		})
		if summary.TotalAllocated.StringFixed(2) != "200.00" {
			t.Fatalf("TotalAllocated = %s, want 200.00", summary.TotalAllocated.StringFixed(2))
		}
		if summary.ReadyToAssign.StringFixed(2) != "3800.00" {
			t.Fatalf("ReadyToAssign = %s, want 3800.00", summary.ReadyToAssign.StringFixed(2))
		}
		if summary.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = false when carryover + effective target covers spend")
		}

		overspent := aggregateMonthSummary(nov, decimal.NewFromInt(4000), decimal.Zero, []categoryMonthAmounts{
			{
				CarryoverBalance: decimal.NewFromInt(450),
				EffectiveTarget:  decimal.NewFromInt(200),
				SpentCleared:     decimal.NewFromInt(700),
				SpentPending:     decimal.Zero,
			},
		})
		if !overspent.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = true when spend exceeds carryover + effective target")
		}
	})

	t.Run("negative rollover adjustment deducts from ReadyToAssign", func(t *testing.T) {
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), decimal.NewFromInt(-45), []categoryMonthAmounts{
			{EffectiveTarget: decimal.NewFromInt(4000), SpentCleared: decimal.Zero, SpentPending: decimal.Zero},
		})
		if summary.RolloverAdjustment.StringFixed(2) != "-45.00" {
			t.Fatalf("RolloverAdjustment = %s, want -45.00", summary.RolloverAdjustment.StringFixed(2))
		}
		if summary.ReadyToAssign.StringFixed(2) != "-45.00" {
			t.Fatalf("ReadyToAssign = %s, want -45.00", summary.ReadyToAssign.StringFixed(2))
		}
	})

	t.Run("positive rollover adjustment adds to ReadyToAssign", func(t *testing.T) {
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), decimal.NewFromInt(50), []categoryMonthAmounts{
			{EffectiveTarget: decimal.NewFromInt(4000), SpentCleared: decimal.Zero, SpentPending: decimal.Zero},
		})
		if summary.RolloverAdjustment.StringFixed(2) != "50.00" {
			t.Fatalf("RolloverAdjustment = %s, want 50.00", summary.RolloverAdjustment.StringFixed(2))
		}
		if summary.ReadyToAssign.StringFixed(2) != "50.00" {
			t.Fatalf("ReadyToAssign = %s, want 50.00", summary.ReadyToAssign.StringFixed(2))
		}
	})
}

func TestComputeMonthSummaryIntegration(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	catRepo := NewCategoryRepo()
	incomeRepo := NewPlannedIncomeRepo()
	baseRepo := NewBaselineRepo()
	ctx := context.Background()
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		if err := incomeRepo.Create(ctx, tx, &MonthlyPlannedIncome{
			WorkspaceID: wsID,
			YearMonth:   nov,
			Name:        "Salary",
			Amount:      decimal.NewFromInt(4000),
		}); err != nil {
			return err
		}

		cat1 := &Category{
			WorkspaceID:   wsID,
			Name:          "Rent",
			GroupName:     "Housing",
			MonthlyBudget: decimal.NewFromInt(2500),
			RolloverMode:  RolloverSweep,
			TargetCadence: "NONE",
		}
		cat2 := &Category{
			WorkspaceID:   wsID,
			Name:          "Groceries",
			GroupName:     "Food",
			MonthlyBudget: decimal.NewFromInt(1500),
			RolloverMode:  RolloverSweep,
			TargetCadence: "NONE",
		}
		if err := catRepo.Create(ctx, tx, cat1); err != nil {
			return err
		}
		if err := catRepo.Create(ctx, tx, cat2); err != nil {
			return err
		}
		if err := baseRepo.InitializeMonth(ctx, tx, nov); err != nil {
			return err
		}

		summary, err := ComputeMonthSummary(ctx, tx, nov)
		if err != nil {
			return err
		}
		if summary.ReadyToAssign.StringFixed(2) != "0.00" || summary.IsUnbalanced {
			t.Fatalf("unexpected summary before overspend: %+v", summary)
		}

		// Create an account and record a $1550 expense on Groceries ($50 overspend)
		acctID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, workspace_id, name, type, current_balance)
			VALUES ($1, $2, 'Checking', 'checking', 5000.00)
		`, acctID, wsID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, amount, merchant_raw, transaction_date, status)
			VALUES ($1, $2, $3, -1550.00, 'Whole Foods', '2026-11-10', 'POSTED')
		`, wsID, acctID, cat2.ID); err != nil {
			return err
		}

		summary2, err := ComputeMonthSummary(ctx, tx, nov)
		if err != nil {
			return err
		}
		if !summary2.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = true after $50 overspend")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestComputeMonthSummaryIntegration failed: %v", err)
	}
}

func TestComputeMonthDetails_CarryoverAndRolloverAdjustments(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	catRepo := NewCategoryRepo()
	incomeRepo := NewPlannedIncomeRepo()
	ctx := context.Background()
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		if err := incomeRepo.Create(ctx, tx, &MonthlyPlannedIncome{
			WorkspaceID: wsID,
			YearMonth:   nov,
			Name:        "Salary",
			Amount:      decimal.NewFromInt(4000),
		}); err != nil {
			return err
		}

		cat := &Category{
			WorkspaceID:   wsID,
			Name:          "Auto Maintenance",
			GroupName:     "Sinking Funds",
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverBalance,
			TargetCadence: "NONE",
		}
		if err := catRepo.Create(ctx, tx, cat); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned, carryover_balance)
			VALUES ($1, $2, $3::date, 200.00, 450.00)
		`, wsID, cat.ID, nov); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_rollover_adjustments
				(workspace_id, source_month, target_month, category_id, adjustment_type, amount, is_active, reason)
			VALUES
				($1, $2::date, $3::date, $4, 'SWEEP_TO_POOL', 50.00, TRUE, 'Swept prior surplus'),
				($1, $2::date, $3::date, NULL, 'CC_DEFICIT_DEDUCTION', -45.00, TRUE, 'Active CC deficit'),
				($1, $2::date, $3::date, NULL, 'CC_DEFICIT_DEDUCTION', -100.00, FALSE, 'Converted to debt baseline')
		`, wsID, oct, nov, cat.ID); err != nil {
			return err
		}

		acctID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, workspace_id, name, type, current_balance)
			VALUES ($1, $2, 'Checking', 'checking', 5000.00)
		`, acctID, wsID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, amount, merchant_raw, transaction_date, status)
			VALUES ($1, $2, $3, -500.00, 'Auto Shop', '2026-11-12', 'POSTED')
		`, wsID, acctID, cat.ID); err != nil {
			return err
		}

		details, err := ComputeMonthDetails(ctx, tx, nov)
		if err != nil {
			return err
		}
		if len(details.Categories) != 1 {
			t.Fatalf("expected 1 category, got %d", len(details.Categories))
		}
		gotCat := details.Categories[0]
		if gotCat.CarryoverBalance.StringFixed(2) != "450.00" {
			t.Fatalf("CarryoverBalance = %s, want 450.00", gotCat.CarryoverBalance.StringFixed(2))
		}
		if gotCat.EffectiveTarget.StringFixed(2) != "200.00" {
			t.Fatalf("EffectiveTarget = %s, want 200.00", gotCat.EffectiveTarget.StringFixed(2))
		}
		if gotCat.Remaining.StringFixed(2) != "150.00" {
			t.Fatalf("Remaining = %s, want 150.00", gotCat.Remaining.StringFixed(2))
		}
		if details.Summary.TotalAllocated.StringFixed(2) != "200.00" {
			t.Fatalf("TotalAllocated = %s, want 200.00", details.Summary.TotalAllocated.StringFixed(2))
		}
		if details.Summary.RolloverAdjustment.StringFixed(2) != "5.00" {
			t.Fatalf("RolloverAdjustment = %s, want 5.00", details.Summary.RolloverAdjustment.StringFixed(2))
		}
		if details.Summary.ReadyToAssign.StringFixed(2) != "3805.00" {
			t.Fatalf("ReadyToAssign = %s, want 3805.00", details.Summary.ReadyToAssign.StringFixed(2))
		}
		if details.Summary.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = false")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestComputeMonthDetails_CarryoverAndRolloverAdjustments failed: %v", err)
	}
}
