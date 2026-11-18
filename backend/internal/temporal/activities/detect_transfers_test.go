package activities

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestFindTransferPairs(t *testing.T) {
	checkingID := uuid.New()
	savingsID := uuid.New()
	outID := uuid.New()
	inID := uuid.New()
	baseDate := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	t.Run("pairs -$500 checking and +$500 savings 1 day apart", func(t *testing.T) {
		pairs := FindTransferPairs([]TxRecord{
			{
				ID:              outID,
				AccountID:       checkingID,
				Amount:          decimal.NewFromFloat(-500.00),
				TransactionDate: baseDate,
			},
			{
				ID:              inID,
				AccountID:       savingsID,
				Amount:          decimal.NewFromFloat(500.00),
				TransactionDate: baseDate.AddDate(0, 0, 1),
			},
		})
		if len(pairs) != 1 {
			t.Fatalf("expected 1 pair, got %d", len(pairs))
		}
		if pairs[0].OutflowID != outID || pairs[0].InflowID != inID {
			t.Fatalf("unexpected pair: %+v", pairs[0])
		}
	})

	t.Run("ignores pairs more than 3 days apart", func(t *testing.T) {
		pairs := FindTransferPairs([]TxRecord{
			{
				ID:              outID,
				AccountID:       checkingID,
				Amount:          decimal.NewFromFloat(-500.00),
				TransactionDate: baseDate,
			},
			{
				ID:              inID,
				AccountID:       savingsID,
				Amount:          decimal.NewFromFloat(500.00),
				TransactionDate: baseDate.AddDate(0, 0, 5),
			},
		})
		if len(pairs) != 0 {
			t.Fatalf("expected 0 pairs, got %d", len(pairs))
		}
	})
}
