package activities

import (
	"math"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/fuzzy"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TxRecord struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	ExternalID      string
	Amount          decimal.Decimal
	MerchantRaw     string
	MerchantClean   string
	TransactionDate time.Time
	Status          string
	Origin          string
}

type MatchCandidate struct {
	ExistingID uuid.UUID
	IsMatched  bool
	TipDelta   decimal.Decimal
}

func isWithinTipMargin(pendingAmt, postedAmt decimal.Decimal) (bool, decimal.Decimal) {
	// Must have the same sign (both outflows or both inflows) and non-zero pending.
	if pendingAmt.IsZero() || pendingAmt.Sign() != postedAmt.Sign() {
		return false, decimal.Zero
	}
	absPending := pendingAmt.Abs()
	absPosted := postedAmt.Abs()

	if absPosted.Equal(absPending) {
		return true, decimal.Zero
	}
	if absPosted.LessThan(absPending) {
		return false, decimal.Zero
	}

	// Posted amount may be 0% to 30% higher than pending authorization.
	maxAllowed := absPending.Mul(decimal.NewFromFloat(1.30))
	if absPosted.LessThanOrEqual(maxAllowed) {
		return true, absPosted.Sub(absPending).Round(2)
	}
	return false, decimal.Zero
}

func merchantSimilarity(a, b string) float64 {
	na := strings.ToLower(strings.TrimSpace(a))
	nb := strings.ToLower(strings.TrimSpace(b))
	if na == "" || nb == "" {
		return 0
	}
	sim := fuzzy.Similarity(na, nb)
	if sim >= 0.80 {
		return sim
	}
	// Handle location/branch suffixes appended on settlement (e.g. "Burger Bar" -> "BURGER BAR DOWNTOWN").
	if strings.HasPrefix(nb, na+" ") || strings.HasPrefix(na, nb+" ") {
		return 0.85
	}
	return sim
}

// MatchPostedToPending matches an incoming posted transaction against a pool of pending authorizations.
func MatchPostedToPending(posted TxRecord, pendingPool []TxRecord) MatchCandidate {
	for _, pending := range pendingPool {
		if pending.AccountID != posted.AccountID {
			continue
		}

		daysDiff := math.Abs(posted.TransactionDate.Sub(pending.TransactionDate).Hours() / 24.0)
		if daysDiff > 4.0 {
			continue
		}

		pName := pending.MerchantClean
		if pName == "" {
			pName = pending.MerchantRaw
		}
		oName := posted.MerchantClean
		if oName == "" {
			oName = posted.MerchantRaw
		}
		if merchantSimilarity(pName, oName) < 0.80 {
			continue
		}

		ok, tipDelta := isWithinTipMargin(pending.Amount, posted.Amount)
		if !ok {
			continue
		}

		return MatchCandidate{
			ExistingID: pending.ID,
			IsMatched:  true,
			TipDelta:   tipDelta,
		}
	}
	return MatchCandidate{IsMatched: false}
}
