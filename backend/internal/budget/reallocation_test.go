package budget

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestReallocationValidation(t *testing.T) {
	svc := NewReallocationService(nil, nil, nil)
	ctx := context.Background()
	idA := uuid.New()
	idB := uuid.New()
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	t.Run("rejects zero or negative amount", func(t *testing.T) {
		err := svc.TransferFunds(ctx, nil, ReallocationRequest{
			YearMonth:      nov,
			FromCategoryID: idA,
			ToCategoryID:   idB,
			Amount:         decimal.Zero,
		})
		if !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("expected ErrInvalidAmount, got %v", err)
		}

		err = svc.TransferFunds(ctx, nil, ReallocationRequest{
			YearMonth:      nov,
			FromCategoryID: idA,
			ToCategoryID:   idB,
			Amount:         decimal.NewFromFloat(-10.00),
		})
		if !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("expected ErrInvalidAmount for negative amount, got %v", err)
		}
	})

	t.Run("rejects identical source and destination categories", func(t *testing.T) {
		err := svc.TransferFunds(ctx, nil, ReallocationRequest{
			YearMonth:      nov,
			FromCategoryID: idA,
			ToCategoryID:   idA,
			Amount:         decimal.NewFromFloat(25.00),
		})
		if !errors.Is(err, ErrSameCategoryTransfer) {
			t.Fatalf("expected ErrSameCategoryTransfer, got %v", err)
		}
	})
}

func TestReallocationIntegration(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	catRepo := NewCategoryRepo()
	baseRepo := NewBaselineRepo()
	reallocRepo := NewReallocationRepo()
	svc := NewReallocationService(catRepo, baseRepo, reallocRepo)
	ctx := context.Background()
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		catA := &Category{
			WorkspaceID:   wsID,
			Name:          "Dining",
			GroupName:     "Food",
			MonthlyBudget: decimal.NewFromFloat(300.00),
			RolloverMode:  RolloverSweep,
			TargetCadence: "NONE",
		}
		catB := &Category{
			WorkspaceID:   wsID,
			Name:          "Groceries",
			GroupName:     "Food",
			MonthlyBudget: decimal.NewFromFloat(100.00),
			RolloverMode:  RolloverSweep,
			TargetCadence: "NONE",
		}
		if err := catRepo.Create(ctx, tx, catA); err != nil {
			return err
		}
		if err := catRepo.Create(ctx, tx, catB); err != nil {
			return err
		}

		if err := svc.TransferFunds(ctx, tx, ReallocationRequest{
			YearMonth:      nov,
			FromCategoryID: catA.ID,
			ToCategoryID:   catB.ID,
			Amount:         decimal.NewFromFloat(50.00),
			Reason:         "Cover groceries overspend",
		}); err != nil {
			return err
		}

		effA, err := svc.GetEffectiveTarget(ctx, tx, catA.ID, nov)
		if err != nil {
			return err
		}
		effB, err := svc.GetEffectiveTarget(ctx, tx, catB.ID, nov)
		if err != nil {
			return err
		}

		if effA.StringFixed(2) != "250.00" {
			t.Fatalf("expected catA effective target 250.00, got %s", effA.StringFixed(2))
		}
		if effB.StringFixed(2) != "150.00" {
			t.Fatalf("expected catB effective target 150.00, got %s", effB.StringFixed(2))
		}
		if effA.Add(effB).StringFixed(2) != "400.00" {
			t.Fatalf("expected total allocated 400.00, got %s", effA.Add(effB).StringFixed(2))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestReallocationIntegration failed: %v", err)
	}
}
