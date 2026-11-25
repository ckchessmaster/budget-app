package transaction

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TestCreateTransactionIdempotent covers the rule the durable replay depends on: a retried
// item reports success without writing a second row, and — because the balance adjustment
// follows the insert in the same transaction — without repeating that either. The `applied`
// boolean is what lets the caller tell those two cases apart.
func TestCreateTransactionIdempotent(t *testing.T) {
	key := "offline-2f2a1c48"
	acctID := uuid.New()

	t.Run("a replayed key returns the existing row and inserts nothing", func(t *testing.T) {
		existing := &Transaction{
			ID:          uuid.New(),
			AccountID:   acctID,
			ClientID:    &key,
			Amount:      decimal.NewFromFloat(-9.25),
			MerchantRaw: "Cafe",
		}
		repo := &stubTxRepo{tx: existing}

		got, applied, err := NewService(repo, nil).CreateTransactionIdempotent(context.Background(), nil,
			CreateSplitTransactionRequest{
				AccountID:   acctID,
				TotalAmount: decimal.NewFromFloat(-9.25),
				MerchantRaw: "Cafe",
				ClientID:    key,
			})
		if err != nil {
			t.Fatalf("a duplicate must report success, got %v", err)
		}
		if applied {
			t.Fatalf("applied = true for a key that was already stored")
		}
		if got.ID != existing.ID {
			t.Fatalf("returned transaction = %s, want the existing %s", got.ID, existing.ID)
		}
		if repo.creates != 0 {
			t.Fatalf("Create called %d times for a replayed key, want 0", repo.creates)
		}
	})

	t.Run("an unseen key inserts and reports applied", func(t *testing.T) {
		repo := &stubTxRepo{}

		_, applied, err := NewService(repo, nil).CreateTransactionIdempotent(context.Background(), nil,
			CreateSplitTransactionRequest{
				AccountID:   acctID,
				TotalAmount: decimal.NewFromFloat(-4.00),
				MerchantRaw: "Bus",
				ClientID:    key,
			})
		if err != nil {
			t.Fatalf("CreateTransactionIdempotent: %v", err)
		}
		if !applied {
			t.Fatalf("applied = false for a key that had not been stored")
		}
		if repo.creates != 1 {
			t.Fatalf("Create called %d times, want 1", repo.creates)
		}
	})

	t.Run("no key behaves like the synchronous create", func(t *testing.T) {
		repo := &stubTxRepo{}

		_, applied, err := NewService(repo, nil).CreateTransactionIdempotent(context.Background(), nil,
			CreateSplitTransactionRequest{
				AccountID:   acctID,
				TotalAmount: decimal.NewFromFloat(-4.00),
				MerchantRaw: "Bus",
			})
		if err != nil {
			t.Fatalf("CreateTransactionIdempotent: %v", err)
		}
		if !applied || repo.creates != 1 {
			t.Fatalf("applied=%v creates=%d, want a plain insert", applied, repo.creates)
		}
	})

	t.Run("validation still rejects before any lookup", func(t *testing.T) {
		repo := &stubTxRepo{}

		if _, _, err := NewService(repo, nil).CreateTransactionIdempotent(context.Background(), nil,
			CreateSplitTransactionRequest{AccountID: uuid.Nil, MerchantRaw: "Cafe", ClientID: key}); err == nil {
			t.Fatalf("expected a missing account to be rejected")
		}
		if repo.creates != 0 {
			t.Fatalf("Create called %d times for an invalid request, want 0", repo.creates)
		}
	})
}
