package categorization

import (
	"regexp"
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

func normalizeMatchText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// PatternMatches checks whether pattern matches either merchant_clean or merchant_raw case-insensitively.
func PatternMatches(matchType, pattern, merchantRaw string, merchantClean *string) bool {
	mt := strings.ToUpper(strings.TrimSpace(matchType))
	if mt == "" {
		mt = MatchTypeContains
	}

	rawPat := strings.TrimSpace(pattern)
	if rawPat == "" {
		return false
	}

	if mt == MatchTypeRegex {
		re, err := regexp.Compile("(?i)" + rawPat)
		if err != nil {
			return false
		}
		if merchantClean != nil {
			if c := strings.TrimSpace(*merchantClean); c != "" && re.MatchString(c) {
				return true
			}
		}
		if r := strings.TrimSpace(merchantRaw); r != "" && re.MatchString(r) {
			return true
		}
		return false
	}

	pat := normalizeMatchText(rawPat)
	if pat == "" {
		return false
	}

	var candidates [2]string
	n := 0
	if merchantClean != nil {
		if c := normalizeMatchText(*merchantClean); c != "" {
			candidates[n] = c
			n++
		}
	}
	if r := normalizeMatchText(merchantRaw); r != "" {
		candidates[n] = r
		n++
	}

	for i := 0; i < n; i++ {
		switch mt {
		case MatchTypeExact:
			if candidates[i] == pat {
				return true
			}
		case MatchTypeStartsWith:
			if strings.HasPrefix(candidates[i], pat) {
				return true
			}
		case MatchTypeEndsWith:
			if strings.HasSuffix(candidates[i], pat) {
				return true
			}
		case MatchTypeContains:
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
