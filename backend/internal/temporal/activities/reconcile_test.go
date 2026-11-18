package activities

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestMatchPostedToPending(t *testing.T) {
	acctID := uuid.New()
	pendingID := uuid.New()
	baseDate := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	t.Run("matches pending $40 Burger Bar to posted $48.50 BURGER BAR DOWNTOWN", func(t *testing.T) {
		pool := []TxRecord{
			{
				ID:              pendingID,
				AccountID:       acctID,
				Amount:          decimal.NewFromFloat(-40.00),
				MerchantRaw:     "Burger Bar",
				TransactionDate: baseDate,
				Status:          "PENDING",
			},
		}
		posted := TxRecord{
			AccountID:       acctID,
			ExternalID:      "ext-1",
			Amount:          decimal.NewFromFloat(-48.50),
			MerchantRaw:     "BURGER BAR DOWNTOWN",
			TransactionDate: baseDate.AddDate(0, 0, 2),
			Status:          "POSTED",
		}

		cand := MatchPostedToPending(posted, pool)
		if !cand.IsMatched {
			t.Fatalf("expected match, got IsMatched=false")
		}
		if cand.ExistingID != pendingID {
			t.Fatalf("ExistingID = %s, want %s", cand.ExistingID, pendingID)
		}
		if cand.TipDelta.StringFixed(2) != "8.50" {
			t.Fatalf("TipDelta = %s, want 8.50", cand.TipDelta.StringFixed(2))
		}
	})

	t.Run("does not match pending $40 Target to posted $65 Target (>30% tip margin)", func(t *testing.T) {
		pool := []TxRecord{
			{
				ID:              pendingID,
				AccountID:       acctID,
				Amount:          decimal.NewFromFloat(-40.00),
				MerchantRaw:     "Target",
				TransactionDate: baseDate,
				Status:          "PENDING",
			},
		}
		posted := TxRecord{
			AccountID:       acctID,
			ExternalID:      "ext-2",
			Amount:          decimal.NewFromFloat(-65.00),
			MerchantRaw:     "Target",
			TransactionDate: baseDate.AddDate(0, 0, 1),
			Status:          "POSTED",
		}

		cand := MatchPostedToPending(posted, pool)
		if cand.IsMatched {
			t.Fatalf("expected no match when posted exceeds 30%% margin, got %+v", cand)
		}
	})
}
