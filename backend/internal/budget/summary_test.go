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
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), []categoryMonthAmounts{
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
		summary := aggregateMonthSummary(nov, decimal.NewFromInt(4000), []categoryMonthAmounts{
			{EffectiveTarget: decimal.NewFromInt(2500), SpentCleared: decimal.NewFromInt(2550), SpentPending: decimal.Zero},
			{EffectiveTarget: decimal.NewFromInt(1500), SpentCleared: decimal.Zero, SpentPending: decimal.Zero},
		})
		if !summary.IsUnbalanced {
			t.Fatalf("expected IsUnbalanced = true on $50 overspend")
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
