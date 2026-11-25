package categorization

import (
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type MatchableTx struct {
	MerchantRaw   string
	MerchantClean *string
	AccountID     uuid.UUID
	Amount        decimal.Decimal
}

// PatternMatches checks whether pattern matches either merchant_clean or merchant_raw case-insensitively.
func PatternMatches(matchType, pattern, merchantRaw string, merchantClean *string) bool {
	pat := strings.ToLower(strings.TrimSpace(pattern))
	if pat == "" {
		return false
	}

	var candidates [2]string
	n := 0
	if merchantClean != nil {
		if c := strings.ToLower(strings.TrimSpace(*merchantClean)); c != "" {
			candidates[n] = c
			n++
		}
	}
	if r := strings.ToLower(strings.TrimSpace(merchantRaw)); r != "" {
		candidates[n] = r
		n++
	}

	exact := strings.EqualFold(strings.TrimSpace(matchType), MatchTypeExact)
	for i := 0; i < n; i++ {
		if exact {
			if candidates[i] == pat {
				return true
			}
		} else {
			if strings.Contains(candidates[i], pat) {
				return true
			}
		}
	}
	return false
}

// DirectionMatches validates whether txAmount matches EXPENSE, INFLOW, or ANY.
func DirectionMatches(direction string, txAmount decimal.Decimal) bool {
	switch strings.ToUpper(strings.TrimSpace(direction)) {
	case "", DirectionAny:
		return true
	case DirectionExpense:
		return txAmount.Sign() < 0
	case DirectionInflow:
		return txAmount.Sign() > 0
	default:
		return false
	}
}

// RuleMatches evaluates whether an active rule matches the given transaction attributes.
func RuleMatches(rule Rule, tx MatchableTx) bool {
	if !rule.IsActive {
		return false
	}
	if rule.AccountID != nil && *rule.AccountID != uuid.Nil && tx.AccountID != *rule.AccountID {
		return false
	}
	if !DirectionMatches(rule.Direction, tx.Amount) {
		return false
	}

	absAmt := tx.Amount.Abs()
	if rule.MinAmount != nil && absAmt.LessThan(rule.MinAmount.Abs()) {
		return false
	}
	if rule.MaxAmount != nil && absAmt.GreaterThan(rule.MaxAmount.Abs()) {
		return false
	}

	return PatternMatches(rule.MatchType, rule.MerchantPattern, tx.MerchantRaw, tx.MerchantClean)
}
