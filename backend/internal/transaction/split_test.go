package transaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestSplitValidation(t *testing.T) {
	svc := NewTransactionService(nil)
	ctx := context.Background()
	acctID := uuid.New()
	catA := uuid.New()
	catB := uuid.New()

	t.Run("rejects parent -$100.00 with splits [-$60.00, -$39.99]", func(t *testing.T) {
		_, _, err := svc.CreateSplit(ctx, nil, CreateSplitTransactionRequest{
			AccountID:       acctID,
			TotalAmount:     decimal.NewFromFloat(-100.00),
			MerchantRaw:     "Target",
			TransactionDate: time.Now().UTC(),
			Splits: []SplitItem{
				{Amount: decimal.NewFromFloat(-60.00), CategoryID: &catA},
				{Amount: decimal.NewFromFloat(-39.99), CategoryID: &catB},
			},
		})
		if !errors.Is(err, ErrSplitSumMismatch) {
			t.Fatalf("expected ErrSplitSumMismatch, got %v", err)
		}
	})

	t.Run("accepts exact split sum [-$60.00, -$40.00] = -$100.00", func(t *testing.T) {
		err := ValidateSplits(decimal.NewFromFloat(-100.00), []SplitItem{
			{Amount: decimal.NewFromFloat(-60.00), CategoryID: &catA},
			{Amount: decimal.NewFromFloat(-40.00), CategoryID: &catB},
		})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}
