package categorization

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestNormalizeRuleFieldsValidation(t *testing.T) {
	catID := uuid.New()
	incName := "Primary Paycheck"
	minAmt := decimal.NewFromFloat(50)
	maxAmt := decimal.NewFromFloat(10)

	t.Run("requires merchant pattern", func(t *testing.T) {
		_, err := normalizeRuleFields(MatchTypeContains, "   ", DirectionAny, nil, nil, &catID, nil, nil)
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule, got %v", err)
		}
	})

	t.Run("requires XOR category_id or planned_income_name", func(t *testing.T) {
		_, err := normalizeRuleFields(MatchTypeContains, "Coffee", DirectionExpense, nil, nil, &catID, &incName, nil)
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule when both set, got %v", err)
		}
		_, err = normalizeRuleFields(MatchTypeContains, "Coffee", DirectionExpense, nil, nil, nil, nil, nil)
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule when neither set, got %v", err)
		}
	})

	t.Run("rejects min_amount > max_amount", func(t *testing.T) {
		_, err := normalizeRuleFields(MatchTypeContains, "Coffee", DirectionExpense, &minAmt, &maxAmt, &catID, nil, nil)
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule when min > max, got %v", err)
		}
	})

	t.Run("normalizes valid rule with planned income name", func(t *testing.T) {
		r, err := normalizeRuleFields("", "  gusto ", "inflow", nil, nil, nil, &incName, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.MatchType != MatchTypeContains || r.Direction != DirectionInflow || r.MerchantPattern != "gusto" {
			t.Fatalf("unexpected normalized rule: %+v", r)
		}
	})

	t.Run("accepts starts_with, ends_with, and valid regex, rejects invalid regex", func(t *testing.T) {
		for _, mt := range []string{MatchTypeStartsWith, MatchTypeEndsWith, MatchTypeRegex} {
			r, err := normalizeRuleFields(mt, "coffee", DirectionAny, nil, nil, &catID, nil, nil)
			if err != nil {
				t.Fatalf("expected %s to be valid, got %v", mt, err)
			}
			if r.MatchType != mt {
				t.Fatalf("expected match type %s, got %s", mt, r.MatchType)
			}
		}

		_, err := normalizeRuleFields(MatchTypeRegex, "[unclosed", DirectionAny, nil, nil, &catID, nil, nil)
		if !errors.Is(err, ErrInvalidRule) {
			t.Fatalf("expected ErrInvalidRule for invalid regex, got %v", err)
		}
	})
}
