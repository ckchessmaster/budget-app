package activities

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestMatchIncomingBankToManual(t *testing.T) {
	acctID := uuid.New()
	manualID := uuid.New()

	manualPool := []TxRecord{
		{
			ID:              manualID,
			AccountID:       acctID,
			Amount:          decimal.NewFromFloat(-14.50),
			MerchantRaw:     "Coffee",
			TransactionDate: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
			Origin:          "MANUAL_OFFLINE",
		},
	}

	t.Run("promotes matching manual entry within +-2 days", func(t *testing.T) {
		bankTx := TxRecord{
			AccountID:       acctID,
			ExternalID:      "ext_987",
			Amount:          decimal.NewFromFloat(-14.50),
			MerchantRaw:     "SQ *LOCAL COFFEE",
			TransactionDate: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC),
		}

		cand := MatchIncomingBankToManual(bankTx, manualPool)
		if !cand.Matched {
			t.Fatalf("expected match, got Matched=false")
		}
		if cand.ManualTxID != manualID || cand.BankExternalID != "ext_987" {
			t.Fatalf("unexpected candidate: %+v", cand)
		}
	})

	t.Run("does not match when date difference exceeds 2 days", func(t *testing.T) {
		bankTx := TxRecord{
			AccountID:       acctID,
			ExternalID:      "ext_988",
			Amount:          decimal.NewFromFloat(-14.50),
			MerchantRaw:     "SQ *LOCAL COFFEE",
			TransactionDate: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
		}

		cand := MatchIncomingBankToManual(bankTx, manualPool)
		if cand.Matched {
			t.Fatalf("expected no match outside +-2 days, got %+v", cand)
		}
	})
}
