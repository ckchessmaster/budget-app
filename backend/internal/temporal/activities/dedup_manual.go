package activities

import (
	"math"

	"github.com/google/uuid"
)

type ManualMatchCandidate struct {
	ManualTxID     uuid.UUID
	Matched        bool
	BankExternalID string
}

// MatchIncomingBankToManual matches an incoming bank feed transaction against
// existing MANUAL_OFFLINE entries using account ID, +-2 day window, and exact/tip-margin amount.
func MatchIncomingBankToManual(bankTx TxRecord, manualPool []TxRecord) ManualMatchCandidate {
	for _, manual := range manualPool {
		if manual.AccountID != bankTx.AccountID {
			continue
		}

		daysDiff := math.Abs(bankTx.TransactionDate.Sub(manual.TransactionDate).Hours() / 24.0)
		if daysDiff > 2.0 {
			continue
		}

		ok, _ := isWithinTipMargin(manual.Amount, bankTx.Amount)
		if !ok {
			continue
		}

		return ManualMatchCandidate{
			ManualTxID:     manual.ID,
			Matched:        true,
			BankExternalID: bankTx.ExternalID,
		}
	}
	return ManualMatchCandidate{Matched: false}
}
