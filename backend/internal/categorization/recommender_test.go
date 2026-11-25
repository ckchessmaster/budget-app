package categorization

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestComputeSuggestions(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	checkingID := uuid.New()
	petCardID := uuid.New()

	catGroceries := uuid.New()
	catHousehold := uuid.New()
	catCoffee := uuid.New()
	catPets := uuid.New()
	catTravel := uuid.New()
	incPaycheck := uuid.New()

	categories := []RecommendCategory{
		{ID: catGroceries, Name: "Groceries", GroupName: "Essentials"},
		{ID: catHousehold, Name: "Household Supplies", GroupName: "Home"},
		{ID: catCoffee, Name: "Coffee Shops", GroupName: "Dining"},
		{ID: catPets, Name: "Pets", GroupName: "Living"},
		{ID: catTravel, Name: "Travel", GroupName: "Discretionary"},
	}
	incomes := []RecommendPlannedIncome{
		{ID: incPaycheck, Name: "Primary Paycheck"},
	}

	paycheckName := "Primary Paycheck"

	t.Run("no fallback padding on completely unknown merchant", func(t *testing.T) {
		txID := uuid.New()
		res := ComputeSuggestions(RecommendInput{
			Now:            now,
			Categories:     categories,
			PlannedIncomes: incomes,
			Targets: []RecommendTargetTx{
				{ID: txID, AccountID: checkingID, Amount: decimal.NewFromFloat(-42.00), MerchantRaw: "ZXQWQ9981 VENDOR", Date: now},
			},
		})
		if got := res[txID]; len(got) != 0 {
			t.Fatalf("expected 0 suggestions for unknown merchant without padding, got %+v", got)
		}
	})

	t.Run("sign filtering keeps expenses away from planned incomes and surfaces refund matches on inflows", func(t *testing.T) {
		txExpID := uuid.New()
		txRefundID := uuid.New()
		txPayID := uuid.New()

		res := ComputeSuggestions(RecommendInput{
			Now:            now,
			Categories:     categories,
			PlannedIncomes: incomes,
			History: []RecommendHistoricalTx{
				{
					AccountID:   checkingID,
					Amount:      decimal.NewFromFloat(-85.00),
					MerchantRaw: "TRADER JOE'S #12",
					CategoryID:  &catGroceries,
					Date:        now.AddDate(0, 0, -10),
				},
				{
					AccountID:         checkingID,
					Amount:            decimal.NewFromFloat(2500.00),
					MerchantRaw:       "ACME PAYROLL DIRECT DEP",
					PlannedIncomeName: &paycheckName,
					Date:              now.AddDate(0, 0, -14),
				},
			},
			Targets: []RecommendTargetTx{
				{ID: txExpID, AccountID: checkingID, Amount: decimal.NewFromFloat(-50.00), MerchantRaw: "ACME PAYROLL FEE", Date: now},
				{ID: txRefundID, AccountID: checkingID, Amount: decimal.NewFromFloat(85.00), MerchantRaw: "TRADER JOE'S #12", Date: now},
				{ID: txPayID, AccountID: checkingID, Amount: decimal.NewFromFloat(2500.00), MerchantRaw: "ACME PAYROLL DIRECT DEP", Date: now},
			},
		})

		for _, s := range res[txExpID] {
			if s.TargetType == TargetTypePlannedIncome {
				t.Fatalf("expected no PLANNED_INCOME suggestion for expense tx, got %+v", s)
			}
		}

		refundSuggestions := res[txRefundID]
		if len(refundSuggestions) == 0 || refundSuggestions[0].CategoryID == nil || *refundSuggestions[0].CategoryID != catGroceries {
			t.Fatalf("expected Groceries refund match top suggestion, got %+v", refundSuggestions)
		}

		paySuggestions := res[txPayID]
		if len(paySuggestions) == 0 || paySuggestions[0].PlannedIncomeID == nil || *paySuggestions[0].PlannedIncomeID != incPaycheck {
			t.Fatalf("expected Primary Paycheck suggestion for payroll inflow, got %+v", paySuggestions)
		}
	})

	t.Run("amount proximity disambiguates multi-category merchant like Target", func(t *testing.T) {
		txSmall := uuid.New()
		txLarge := uuid.New()
		res := ComputeSuggestions(RecommendInput{
			Now:        now,
			Categories: categories,
			History: []RecommendHistoricalTx{
				// Two older $18 Target coffee runs
				{AccountID: checkingID, Amount: decimal.NewFromFloat(-18.00), MerchantRaw: "TARGET", CategoryID: &catCoffee, Date: now.AddDate(0, 0, -20)},
				{AccountID: checkingID, Amount: decimal.NewFromFloat(-17.50), MerchantRaw: "TARGET", CategoryID: &catCoffee, Date: now.AddDate(0, 0, -12)},
				// Two recent $140 Target household runs
				{AccountID: checkingID, Amount: decimal.NewFromFloat(-142.00), MerchantRaw: "TARGET", CategoryID: &catHousehold, Date: now.AddDate(0, 0, -8)},
				{AccountID: checkingID, Amount: decimal.NewFromFloat(-138.00), MerchantRaw: "TARGET", CategoryID: &catHousehold, Date: now.AddDate(0, 0, -5)},
			},
			Targets: []RecommendTargetTx{
				{ID: txSmall, AccountID: checkingID, Amount: decimal.NewFromFloat(-18.25), MerchantRaw: "TARGET", Date: now},
				{ID: txLarge, AccountID: checkingID, Amount: decimal.NewFromFloat(-140.00), MerchantRaw: "TARGET", Date: now},
			},
		})

		if len(res[txSmall]) == 0 || *res[txSmall][0].CategoryID != catCoffee {
			t.Fatalf("expected $18 Target tx to favor Coffee Shops via amount proximity, got %+v", res[txSmall])
		}
		if len(res[txLarge]) == 0 || *res[txLarge][0].CategoryID != catHousehold {
			t.Fatalf("expected $140 Target tx to favor Household Supplies via amount proximity, got %+v", res[txLarge])
		}
	})

	t.Run("fuzzy merchant match, account affinity, and keyword hints", func(t *testing.T) {
		txFuzzy := uuid.New()
		txAcct := uuid.New()
		txKW := uuid.New()

		var history []RecommendHistoricalTx
		history = append(history, RecommendHistoricalTx{
			AccountID:   checkingID,
			Amount:      decimal.NewFromFloat(-6.50),
			MerchantRaw: "STARBUCKS #412",
			CategoryID:  &catCoffee,
			Date:        now.AddDate(0, 0, -3),
		})
		for i := 0; i < 6; i++ {
			history = append(history, RecommendHistoricalTx{
				AccountID:   petCardID,
				Amount:      decimal.NewFromFloat(-35.00),
				MerchantRaw: "VET CLINIC",
				CategoryID:  &catPets,
				Date:        now.AddDate(0, 0, -(i + 2)),
			})
		}

		res := ComputeSuggestions(RecommendInput{
			Now:        now,
			Categories: categories,
			History:    history,
			Targets: []RecommendTargetTx{
				{ID: txFuzzy, AccountID: checkingID, Amount: decimal.NewFromFloat(-6.25), MerchantRaw: "STARBUCKS #419", Date: now},
				{ID: txAcct, AccountID: petCardID, Amount: decimal.NewFromFloat(-22.00), MerchantRaw: "UNKNOWN BARK BOX", Date: now},
				{ID: txKW, AccountID: checkingID, Amount: decimal.NewFromFloat(-280.00), MerchantRaw: "DELTA AIRLINES", Date: now},
			},
		})

		if len(res[txFuzzy]) == 0 || *res[txFuzzy][0].CategoryID != catCoffee {
			t.Fatalf("expected fuzzy match to suggest Coffee Shops, got %+v", res[txFuzzy])
		}
		if len(res[txAcct]) == 0 || *res[txAcct][0].CategoryID != catPets {
			t.Fatalf("expected account affinity on petCardID to suggest Pets, got %+v", res[txAcct])
		}
		if len(res[txKW]) == 0 || *res[txKW][0].CategoryID != catTravel {
			t.Fatalf("expected keyword match for DELTA AIRLINES to suggest Travel, got %+v", res[txKW])
		}
	})
}
