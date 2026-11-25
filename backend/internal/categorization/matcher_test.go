package categorization

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func strPtr(s string) *string { return &s }

func TestRuleMatches(t *testing.T) {
	acctA := uuid.New()
	acctB := uuid.New()
	min10 := decimal.NewFromFloat(10.00)
	max50 := decimal.NewFromFloat(50.00)

	t.Run("contains match against raw or clean merchant", func(t *testing.T) {
		r := Rule{
			IsActive:        true,
			MatchType:       MatchTypeContains,
			MerchantPattern: "trader joe",
			Direction:       DirectionExpense,
		}
		if !RuleMatches(r, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-42.15),
			MerchantRaw: "TRADER JOE'S #123 SEATTLE",
		}) {
			t.Fatal("expected rule to match raw merchant")
		}
		if !RuleMatches(r, MatchableTx{
			AccountID:     acctA,
			Amount:        decimal.NewFromFloat(-15.00),
			MerchantRaw:   "POS 99812",
			MerchantClean: strPtr("Trader Joes"),
		}) {
			// "trader joes" contains "trader joe"
			t.Fatal("expected rule to match clean merchant")
		}
		if RuleMatches(r, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(42.15),
			MerchantRaw: "TRADER JOE'S REFUND",
		}) {
			t.Fatal("expected EXPENSE direction not to match positive inflow")
		}
	})

	t.Run("exact match, account scope, and amount bounds", func(t *testing.T) {
		r := Rule{
			IsActive:        true,
			MatchType:       MatchTypeExact,
			MerchantPattern: "Netflix",
			AccountID:       &acctA,
			Direction:       DirectionExpense,
			MinAmount:       &min10,
			MaxAmount:       &max50,
		}
		if !RuleMatches(r, MatchableTx{
			AccountID:     acctA,
			Amount:        decimal.NewFromFloat(-15.99),
			MerchantRaw:   "NFLX *SUBSCRIPTION",
			MerchantClean: strPtr("Netflix"),
		}) {
			t.Fatal("expected exact match on clean merchant within account & amount bounds")
		}
		if RuleMatches(r, MatchableTx{
			AccountID:     acctA,
			Amount:        decimal.NewFromFloat(-15.99),
			MerchantRaw:   "Netflix Monthly",
			MerchantClean: strPtr("Netflix Monthly"),
		}) {
			t.Fatal("expected EXACT match not to match longer string")
		}
		if RuleMatches(r, MatchableTx{
			AccountID:     acctB,
			Amount:        decimal.NewFromFloat(-15.99),
			MerchantRaw:   "Netflix",
			MerchantClean: strPtr("Netflix"),
		}) {
			t.Fatal("expected different account not to match")
		}
		if RuleMatches(r, MatchableTx{
			AccountID:     acctA,
			Amount:        decimal.NewFromFloat(-75.00),
			MerchantRaw:   "Netflix",
			MerchantClean: strPtr("Netflix"),
		}) {
			t.Fatal("expected out-of-range amount not to match")
		}
	})

	t.Run("inactive rule never matches", func(t *testing.T) {
		r := Rule{
			IsActive:        false,
			MatchType:       MatchTypeContains,
			MerchantPattern: "coffee",
			Direction:       DirectionAny,
		}
		if RuleMatches(r, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-4.50),
			MerchantRaw: "Corner Coffee",
		}) {
			t.Fatal("expected inactive rule not to match")
		}
	})
}
