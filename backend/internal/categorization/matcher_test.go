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

	t.Run("contains normalizes extra whitespace", func(t *testing.T) {
		r := Rule{
			IsActive:        true,
			MatchType:       MatchTypeContains,
			MerchantPattern: "trader   joe's",
			Direction:       DirectionAny,
		}
		if !RuleMatches(r, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-12.34),
			MerchantRaw: "TRADER  JOE'S   #123",
		}) {
			t.Fatal("expected whitespace-normalized CONTAINS to match")
		}
	})

	t.Run("starts_with, ends_with, and regex operators", func(t *testing.T) {
		startsWithRule := Rule{
			IsActive:        true,
			MatchType:       MatchTypeStartsWith,
			MerchantPattern: "sq *",
			Direction:       DirectionAny,
		}
		if !RuleMatches(startsWithRule, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-5.50),
			MerchantRaw: "SQ *BLUE BOTTLE",
		}) {
			t.Fatal("expected STARTS_WITH to match prefix")
		}
		if RuleMatches(startsWithRule, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-5.50),
			MerchantRaw: "PAYPAL SQ *BLUE BOTTLE",
		}) {
			t.Fatal("expected STARTS_WITH not to match non-prefix")
		}

		endsWithRule := Rule{
			IsActive:        true,
			MatchType:       MatchTypeEndsWith,
			MerchantPattern: "seattle",
			Direction:       DirectionAny,
		}
		if !RuleMatches(endsWithRule, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-18.00),
			MerchantRaw: "TRADER JOE'S SEATTLE",
		}) {
			t.Fatal("expected ENDS_WITH to match suffix")
		}
		if RuleMatches(endsWithRule, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-18.00),
			MerchantRaw: "SEATTLE COFFEE",
		}) {
			t.Fatal("expected ENDS_WITH not to match non-suffix")
		}

		regexRule := Rule{
			IsActive:        true,
			MatchType:       MatchTypeRegex,
			MerchantPattern: `^uber\s*(eats|trip)?`,
			Direction:       DirectionAny,
		}
		if !RuleMatches(regexRule, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-22.00),
			MerchantRaw: "UBER EATS #99",
		}) {
			t.Fatal("expected REGEX to match case-insensitively")
		}
		if RuleMatches(regexRule, MatchableTx{
			AccountID:   acctA,
			Amount:      decimal.NewFromFloat(-22.00),
			MerchantRaw: "LYFT *UBER",
		}) {
			t.Fatal("expected anchored REGEX not to match mid-string")
		}
	})
}
