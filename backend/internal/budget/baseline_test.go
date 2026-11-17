package budget

import (
	"context"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestNormalizeYearMonth(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	input := time.Date(2026, 11, 18, 23, 45, 0, 0, loc)
	got := NormalizeYearMonth(input)
	want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NormalizeYearMonth = %v, want %v", got, want)
	}
}

func TestInitializeMonth_CopiesPreviousMonthBaseline(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	catRepo := NewCategoryRepo()
	baseRepo := NewBaselineRepo()
	svc := NewBaselineService(baseRepo)
	ctx := context.Background()

	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		cat := &Category{
			WorkspaceID:   wsID,
			Name:          "Dining",
			GroupName:     "Food",
			MonthlyBudget: decimal.NewFromFloat(200.00),
			RolloverMode:  RolloverSweep,
			TargetCadence: "NONE",
		}
		if err := catRepo.Create(ctx, tx, cat); err != nil {
			return err
		}

		// Initialize October (copies category.monthly_budget = 200.00)
		if err := svc.InitializeMonth(ctx, tx, oct); err != nil {
			return err
		}

		// Simulate user adjusting October's baseline to 350.00
		if _, err := tx.Exec(ctx, `
			UPDATE monthly_category_budgets SET original_planned = 350.00
			WHERE category_id = $1 AND year_month = $2::date
		`, cat.ID, oct); err != nil {
			return err
		}

		// Initialize November (should copy October's 350.00, not category's 200.00)
		if err := svc.InitializeMonth(ctx, tx, nov); err != nil {
			return err
		}

		novBaselines, err := svc.GetMonthlyBaselines(ctx, tx, nov)
		if err != nil {
			return err
		}
		if len(novBaselines) != 1 || !novBaselines[0].OriginalPlanned.Equal(decimal.NewFromFloat(350.00)) {
			t.Fatalf("expected November baseline 350.00, got %+v", novBaselines)
		}

		// Adjust November to 400.00 and re-run InitializeMonth: must NOT overwrite
		if _, err := tx.Exec(ctx, `
			UPDATE monthly_category_budgets SET original_planned = 400.00
			WHERE category_id = $1 AND year_month = $2::date
		`, cat.ID, nov); err != nil {
			return err
		}
		if err := svc.InitializeMonth(ctx, tx, nov); err != nil {
			return err
		}

		novBaselines2, err := svc.GetMonthlyBaselines(ctx, tx, nov)
		if err != nil {
			return err
		}
		if len(novBaselines2) != 1 || !novBaselines2[0].OriginalPlanned.Equal(decimal.NewFromFloat(400.00)) {
			t.Fatalf("expected November baseline to remain 400.00, got %+v", novBaselines2)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("TestInitializeMonth_CopiesPreviousMonthBaseline failed: %v", err)
	}
}
