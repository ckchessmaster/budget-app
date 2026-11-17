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
	t.Run("rejects zero or negative amount without DB", func(t *testing.T) {
		repo := NewPlannedIncomeRepo()
		ctx := context.Background()
		nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

		err := repo.Create(ctx, nil, &MonthlyPlannedIncome{
			YearMonth: nov,
			Name:      "Zero Paycheck",
			Amount:    decimal.Zero,
		})
		if !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("expected ErrInvalidAmount for 0.00, got %v", err)
		}

		err = repo.Create(ctx, nil, &MonthlyPlannedIncome{
			YearMonth: nov,
			Name:      "Negative Paycheck",
			Amount:    decimal.NewFromFloat(-50.00),
		})
		if !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("expected ErrInvalidAmount for -50.00, got %v", err)
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
			if err := repo.Create(ctx, tx, inc1); err != nil {
				return err
			}
			if err := repo.Create(ctx, tx, inc2); err != nil {
				return err
			}

			list, err := repo.ListByMonth(ctx, tx, nov)
			if err != nil {
				return err
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
			if len(listAfter) != 1 {
				t.Fatalf("expected 1 income after delete, got %d", len(listAfter))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("TestPlannedIncomeRepo DB test failed: %v", err)
		}
	})
}
