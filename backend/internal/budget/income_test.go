package budget

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestPlannedIncomeRepo(t *testing.T) {
	t.Run("accepts zero and rejects negative amount without DB", func(t *testing.T) {
		repo := NewPlannedIncomeRepo()
		ctx := context.Background()
		nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

		reqZero := CreatePlannedIncomeRequest{
			YearMonth: "2026-11-01",
			Name:      "Unplanned Income",
			Amount:    decimal.Zero,
		}
		if err := reqZero.Validate(); err != nil {
			t.Fatalf("expected zero planned income to be valid, got %v", err)
		}

		reqNeg := CreatePlannedIncomeRequest{
			YearMonth: "2026-11-01",
			Name:      "Negative Paycheck",
			Amount:    decimal.NewFromFloat(-1.00),
		}
		if err := reqNeg.Validate(); !errors.Is(err, ErrNegativeAmount) {
			t.Fatalf("expected ErrNegativeAmount for -1.00, got %v", err)
		}

		err := repo.Create(ctx, nil, &MonthlyPlannedIncome{
			YearMonth: nov,
			Name:      "Negative Paycheck",
			Amount:    decimal.NewFromFloat(-50.00),
		})
		if !errors.Is(err, ErrNegativeAmount) {
			t.Fatalf("expected ErrNegativeAmount for -50.00, got %v", err)
		}
	})

	t.Run("DB CRUD lifecycle", func(t *testing.T) {
		_, appPool, wsID, userID := openTestDB(t)
		repo := NewPlannedIncomeRepo()
		ctx := context.Background()
		nov := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

		err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
			inc1 := &MonthlyPlannedIncome{
				WorkspaceID: wsID,
				YearMonth:   nov,
				Name:        "Paycheck 1",
				Amount:      decimal.NewFromFloat(2500.00),
			}
			inc2 := &MonthlyPlannedIncome{
				WorkspaceID: wsID,
				YearMonth:   nov,
				Name:        "Paycheck 2",
				Amount:      decimal.NewFromFloat(2500.00),
			}
			incZero := &MonthlyPlannedIncome{
				WorkspaceID: wsID,
				YearMonth:   nov,
				Name:        "Unplanned Income",
				Amount:      decimal.Zero,
			}
			if err := repo.Create(ctx, tx, inc1); err != nil {
				return err
			}
			if err := repo.Create(ctx, tx, inc2); err != nil {
				return err
			}
			if err := repo.Create(ctx, tx, incZero); err != nil {
				return err
			}

			list, err := repo.ListByMonth(ctx, tx, nov)
			if err != nil {
				return err
			}
			if len(list) != 3 {
				t.Fatalf("expected 3 incomes, got %d", len(list))
			}
			total := decimal.Zero
			for _, item := range list {
				total = total.Add(item.Amount)
			}
			if total.StringFixed(2) != "5000.00" {
				t.Fatalf("expected total 5000.00, got %s", total.StringFixed(2))
			}

			if err := repo.Delete(ctx, tx, inc1.ID); err != nil {
				return err
			}

			listAfter, err := repo.ListByMonth(ctx, tx, nov)
			if err != nil {
				return err
			}
			if len(listAfter) != 2 {
				t.Fatalf("expected 2 incomes after delete, got %d", len(listAfter))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("TestPlannedIncomeRepo DB test failed: %v", err)
		}
	})
}
