package budget

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func assertDecimalEqual(t *testing.T, field string, got, want decimal.Decimal) {
	t.Helper()
	if got.StringFixed(2) != want.StringFixed(2) {
		t.Fatalf("%s = %s, want %s", field, got.StringFixed(2), want.StringFixed(2))
	}
}

func TestEvaluateCategoryRollover(t *testing.T) {
	targetCatID := uuid.New()
	cap1000 := decimal.NewFromInt(1000)

	t.Run("sinking fund ROLLOVER_BALANCE with 450 remaining carries 450 forward", func(t *testing.T) {
		catID := uuid.New()
		cat := Category{
			ID:            catID,
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverBalance,
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(450))
		if res.CategoryID != catID {
			t.Fatalf("CategoryID = %s, want %s", res.CategoryID, catID)
		}
		if res.Mode != RolloverBalance {
			t.Fatalf("Mode = %s, want %s", res.Mode, RolloverBalance)
		}
		assertDecimalEqual(t, "PriorRemaining", res.PriorRemaining, decimal.NewFromInt(450))
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.NewFromInt(450))
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(200))
		assertDecimalEqual(t, "NextMonthAvailable", res.NextMonthAvailable, decimal.NewFromInt(650))
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.Zero)
		assertDecimalEqual(t, "SweptToPool", res.SweptToPool, decimal.Zero)
		assertDecimalEqual(t, "DeficitAmount", res.DeficitAmount, decimal.Zero)
		if res.SweepTargetID != nil {
			t.Fatalf("expected SweepTargetID = nil, got %s", *res.SweepTargetID)
		}
	})

	t.Run("sinking fund ROLLOVER_BALANCE with zero remaining", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverBalance,
		}

		res := EvaluateCategoryRollover(cat, decimal.Zero)
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(200))
		assertDecimalEqual(t, "NextMonthAvailable", res.NextMonthAvailable, decimal.NewFromInt(200))
		assertDecimalEqual(t, "DeficitAmount", res.DeficitAmount, decimal.Zero)
	})

	t.Run("sweep fund to assignable pool with 50 remaining", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(500),
			RolloverMode:  RolloverSweep,
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(50))
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(500))
		assertDecimalEqual(t, "NextMonthAvailable", res.NextMonthAvailable, decimal.NewFromInt(500))
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.NewFromInt(50))
		assertDecimalEqual(t, "SweptToPool", res.SweptToPool, decimal.NewFromInt(50))
		assertDecimalEqual(t, "DeficitAmount", res.DeficitAmount, decimal.Zero)
		if res.SweepTargetID != nil {
			t.Fatalf("expected SweepTargetID = nil, got %s", *res.SweepTargetID)
		}
	})

	t.Run("sweep fund to designated target category with 50 remaining", func(t *testing.T) {
		cat := Category{
			ID:                    uuid.New(),
			MonthlyBudget:         decimal.NewFromInt(500),
			RolloverMode:          RolloverSweep,
			SweepTargetCategoryID: &targetCatID,
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(50))
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.NewFromInt(50))
		assertDecimalEqual(t, "SweptToPool", res.SweptToPool, decimal.Zero)
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(500))
		if res.SweepTargetID == nil || *res.SweepTargetID != targetCatID {
			t.Fatalf("SweepTargetID = %v, want %s", res.SweepTargetID, targetCatID)
		}
		if res.SweepTargetID == cat.SweepTargetCategoryID {
			t.Fatalf("SweepTargetID must not alias input pointer")
		}
	})

	t.Run("sweep fund cyclic self-reference falls back to pool", func(t *testing.T) {
		catID := uuid.New()
		cat := Category{
			ID:                    catID,
			MonthlyBudget:         decimal.NewFromInt(500),
			RolloverMode:          RolloverSweep,
			SweepTargetCategoryID: &catID,
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(50))
		if res.SweepTargetID != nil {
			t.Fatalf("expected SweepTargetID = nil on self-reference, got %s", *res.SweepTargetID)
		}
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.NewFromInt(50))
		assertDecimalEqual(t, "SweptToPool", res.SweptToPool, decimal.NewFromInt(50))
	})

	t.Run("sweep fund with zero remaining sweeps nothing", func(t *testing.T) {
		cat := Category{
			ID:                    uuid.New(),
			MonthlyBudget:         decimal.NewFromInt(500),
			RolloverMode:          RolloverSweep,
			SweepTargetCategoryID: &targetCatID,
		}

		res := EvaluateCategoryRollover(cat, decimal.Zero)
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.Zero)
		assertDecimalEqual(t, "SweptToPool", res.SweptToPool, decimal.Zero)
		if res.SweepTargetID != nil {
			t.Fatalf("expected SweepTargetID = nil when remaining is zero, got %s", *res.SweepTargetID)
		}
	})

	t.Run("TARGET_CAP partial spend replenishes difference up to cap", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverTarget,
			TargetAmount:  &cap1000,
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(700))
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.NewFromInt(700))
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(300))
		assertDecimalEqual(t, "NextMonthAvailable", res.NextMonthAvailable, decimal.NewFromInt(1000))
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.Zero)
	})

	t.Run("TARGET_CAP full spend replenishes full cap", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverTarget,
			TargetAmount:  &cap1000,
		}

		res := EvaluateCategoryRollover(cat, decimal.Zero)
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(1000))
		assertDecimalEqual(t, "NextMonthAvailable", res.NextMonthAvailable, decimal.NewFromInt(1000))
	})

	t.Run("TARGET_CAP at or above cap assigns zero", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverTarget,
			TargetAmount:  &cap1000,
		}

		atCap := EvaluateCategoryRollover(cat, decimal.NewFromInt(1000))
		assertDecimalEqual(t, "CarryoverAmount", atCap.CarryoverAmount, decimal.NewFromInt(1000))
		assertDecimalEqual(t, "NextMonthAssigned", atCap.NextMonthAssigned, decimal.Zero)
		assertDecimalEqual(t, "NextMonthAvailable", atCap.NextMonthAvailable, decimal.NewFromInt(1000))
		assertDecimalEqual(t, "SweptAmount", atCap.SweptAmount, decimal.Zero)

		aboveCap := EvaluateCategoryRollover(cat, decimal.NewFromInt(1250))
		assertDecimalEqual(t, "CarryoverAmount", aboveCap.CarryoverAmount, decimal.NewFromInt(1250))
		assertDecimalEqual(t, "NextMonthAssigned", aboveCap.NextMonthAssigned, decimal.Zero)
		assertDecimalEqual(t, "NextMonthAvailable", aboveCap.NextMonthAvailable, decimal.NewFromInt(1250))
		assertDecimalEqual(t, "SweptAmount", aboveCap.SweptAmount, decimal.Zero)
	})

	t.Run("TARGET_CAP with nil TargetAmount falls back to MonthlyBudget", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(500),
			RolloverMode:  RolloverTarget,
			TargetAmount:  nil,
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(300))
		assertDecimalEqual(t, "CarryoverAmount", res.CarryoverAmount, decimal.NewFromInt(300))
		assertDecimalEqual(t, "NextMonthAssigned", res.NextMonthAssigned, decimal.NewFromInt(200))
		assertDecimalEqual(t, "NextMonthAvailable", res.NextMonthAvailable, decimal.NewFromInt(500))
	})

	t.Run("overspent deficits across all modes avoid double counting", func(t *testing.T) {
		deficitRemaining := decimal.RequireFromString("-45.50")

		sweepRes := EvaluateCategoryRollover(Category{
			ID:                    uuid.New(),
			MonthlyBudget:         decimal.NewFromInt(500),
			RolloverMode:          RolloverSweep,
			SweepTargetCategoryID: &targetCatID,
		}, deficitRemaining)
		assertDecimalEqual(t, "sweep DeficitAmount", sweepRes.DeficitAmount, decimal.RequireFromString("45.50"))
		assertDecimalEqual(t, "sweep CarryoverAmount", sweepRes.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "sweep SweptAmount", sweepRes.SweptAmount, decimal.Zero)
		assertDecimalEqual(t, "sweep SweptToPool", sweepRes.SweptToPool, decimal.Zero)
		assertDecimalEqual(t, "sweep NextMonthAssigned", sweepRes.NextMonthAssigned, decimal.NewFromInt(500))
		if sweepRes.SweepTargetID != nil {
			t.Fatalf("expected SweepTargetID = nil on overspent sweep category")
		}

		rolloverRes := EvaluateCategoryRollover(Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverBalance,
		}, deficitRemaining)
		assertDecimalEqual(t, "rollover DeficitAmount", rolloverRes.DeficitAmount, decimal.RequireFromString("45.50"))
		assertDecimalEqual(t, "rollover CarryoverAmount", rolloverRes.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "rollover NextMonthAssigned", rolloverRes.NextMonthAssigned, decimal.NewFromInt(200))

		targetRes := EvaluateCategoryRollover(Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(200),
			RolloverMode:  RolloverTarget,
			TargetAmount:  &cap1000,
		}, deficitRemaining)
		assertDecimalEqual(t, "target DeficitAmount", targetRes.DeficitAmount, decimal.RequireFromString("45.50"))
		assertDecimalEqual(t, "target CarryoverAmount", targetRes.CarryoverAmount, decimal.Zero)
		assertDecimalEqual(t, "target NextMonthAssigned", targetRes.NextMonthAssigned, decimal.NewFromInt(1000))
		assertDecimalEqual(t, "target NextMonthAvailable", targetRes.NextMonthAvailable, decimal.NewFromInt(1000))
	})

	t.Run("rounding and negative-zero normalization", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(100),
			RolloverMode:  RolloverSweep,
		}

		negZero := EvaluateCategoryRollover(cat, decimal.RequireFromString("-0.001"))
		if negZero.PriorRemaining.String() != "0" {
			t.Fatalf("expected normalized zero, got %q", negZero.PriorRemaining.String())
		}
		assertDecimalEqual(t, "PriorRemaining", negZero.PriorRemaining, decimal.Zero)
		assertDecimalEqual(t, "DeficitAmount", negZero.DeficitAmount, decimal.Zero)

		rounded := EvaluateCategoryRollover(cat, decimal.RequireFromString("49.996"))
		assertDecimalEqual(t, "PriorRemaining", rounded.PriorRemaining, decimal.NewFromInt(50))
		assertDecimalEqual(t, "SweptAmount", rounded.SweptAmount, decimal.NewFromInt(50))
		assertDecimalEqual(t, "SweptToPool", rounded.SweptToPool, decimal.NewFromInt(50))
	})

	t.Run("empty rollover mode defaults to SWEEP", func(t *testing.T) {
		cat := Category{
			ID:            uuid.New(),
			MonthlyBudget: decimal.NewFromInt(300),
			RolloverMode:  "",
		}

		res := EvaluateCategoryRollover(cat, decimal.NewFromInt(25))
		if res.Mode != RolloverSweep {
			t.Fatalf("Mode = %s, want %s", res.Mode, RolloverSweep)
		}
		assertDecimalEqual(t, "SweptAmount", res.SweptAmount, decimal.NewFromInt(25))
		assertDecimalEqual(t, "SweptToPool", res.SweptToPool, decimal.NewFromInt(25))
	})
}

func TestPlanWorkspaceRollover(t *testing.T) {
	diningID := uuid.New()
	groceriesID := uuid.New()
	autoID := uuid.New()
	emergencyID := uuid.New()
	utilitiesID := uuid.New()
	entertainmentID := uuid.New()
	missingTargetID := uuid.New()

	cap1000 := decimal.NewFromInt(1000)

	inputs := []CategoryRolloverInput{
		{
			// 1. SWEEP -> ROLLOVER_BALANCE (auto): $50 swept to auto
			Category: Category{
				ID:                    diningID,
				MonthlyBudget:         decimal.NewFromInt(300),
				RolloverMode:          RolloverSweep,
				SweepTargetCategoryID: &autoID,
			},
			Remaining: decimal.NewFromInt(50),
		},
		{
			// 2. SWEEP -> pool: $75 swept to pool
			Category: Category{
				ID:            groceriesID,
				MonthlyBudget: decimal.NewFromInt(500),
				RolloverMode:  RolloverSweep,
			},
			Remaining: decimal.NewFromInt(75),
		},
		{
			// 3. ROLLOVER_BALANCE (auto): $450 direct carryover + $50 inbound sweep = $500 carryover
			Category: Category{
				ID:            autoID,
				MonthlyBudget: decimal.NewFromInt(200),
				RolloverMode:  RolloverBalance,
			},
			Remaining: decimal.NewFromInt(450),
		},
		{
			// 4. SWEEP -> TARGET_CAP (emergency): $100 swept to emergency
			Category: Category{
				ID:                    utilitiesID,
				MonthlyBudget:         decimal.NewFromInt(250),
				RolloverMode:          RolloverSweep,
				SweepTargetCategoryID: &emergencyID,
			},
			Remaining: decimal.NewFromInt(100),
		},
		{
			// 5. TARGET_CAP (emergency): $700 direct carryover + $100 inbound sweep = $800 carryover, assigned = $200
			Category: Category{
				ID:            emergencyID,
				MonthlyBudget: decimal.NewFromInt(200),
				RolloverMode:  RolloverTarget,
				TargetAmount:  &cap1000,
			},
			Remaining: decimal.NewFromInt(700),
		},
		{
			// 6. SWEEP -> missing/deleted category ID: $30 falls back to pool
			Category: Category{
				ID:                    uuid.New(),
				MonthlyBudget:         decimal.NewFromInt(100),
				RolloverMode:          RolloverSweep,
				SweepTargetCategoryID: &missingTargetID,
			},
			Remaining: decimal.NewFromInt(30),
		},
		{
			// 7. Overspent category: -$45 deficit
			Category: Category{
				ID:            entertainmentID,
				MonthlyBudget: decimal.NewFromInt(150),
				RolloverMode:  RolloverSweep,
			},
			Remaining: decimal.NewFromInt(-45),
		},
	}

	plan := PlanWorkspaceRollover(inputs)

	if len(plan.Results) != len(inputs) {
		t.Fatalf("len(Results) = %d, want %d", len(plan.Results), len(inputs))
	}
	if len(plan.SweepTransfers) != 2 {
		t.Fatalf("len(SweepTransfers) = %d, want 2", len(plan.SweepTransfers))
	}

	// Verify valid sweep transfers
	if plan.SweepTransfers[0].FromCategoryID != diningID || plan.SweepTransfers[0].ToCategoryID != autoID {
		t.Fatalf("unexpected SweepTransfers[0]: %+v", plan.SweepTransfers[0])
	}
	assertDecimalEqual(t, "SweepTransfers[0].Amount", plan.SweepTransfers[0].Amount, decimal.NewFromInt(50))

	if plan.SweepTransfers[1].FromCategoryID != utilitiesID || plan.SweepTransfers[1].ToCategoryID != emergencyID {
		t.Fatalf("unexpected SweepTransfers[1]: %+v", plan.SweepTransfers[1])
	}
	assertDecimalEqual(t, "SweepTransfers[1].Amount", plan.SweepTransfers[1].Amount, decimal.NewFromInt(100))

	// Auto maintenance receives +$50 inbound sweep
	autoRes := plan.Results[2]
	assertDecimalEqual(t, "auto CarryoverAmount", autoRes.CarryoverAmount, decimal.NewFromInt(500))
	assertDecimalEqual(t, "auto NextMonthAssigned", autoRes.NextMonthAssigned, decimal.NewFromInt(200))
	assertDecimalEqual(t, "auto NextMonthAvailable", autoRes.NextMonthAvailable, decimal.NewFromInt(700))

	// Emergency fund receives +$100 inbound sweep and reduces NextMonthAssigned to keep NextMonthAvailable at $1000 cap
	emergencyRes := plan.Results[4]
	assertDecimalEqual(t, "emergency CarryoverAmount", emergencyRes.CarryoverAmount, decimal.NewFromInt(800))
	assertDecimalEqual(t, "emergency NextMonthAssigned", emergencyRes.NextMonthAssigned, decimal.NewFromInt(200))
	assertDecimalEqual(t, "emergency NextMonthAvailable", emergencyRes.NextMonthAvailable, decimal.NewFromInt(1000))

	// Dangling sweep target falls back to pool
	danglingRes := plan.Results[5]
	if danglingRes.SweepTargetID != nil {
		t.Fatalf("expected dangling SweepTargetID = nil, got %s", *danglingRes.SweepTargetID)
	}
	assertDecimalEqual(t, "dangling SweptToPool", danglingRes.SweptToPool, decimal.NewFromInt(30))

	// Totals & conservation invariants:
	// Direct rollovers: 450 (auto) + 700 (emergency) = 1150
	// Swept to categories: 50 (dining->auto) + 100 (utilities->emergency) = 150
	// Swept to pool: 75 (groceries) + 30 (dangling) = 105
	// Positive surplus sum: 50 + 75 + 450 + 100 + 700 + 30 = 1405 == 1150 + 150 + 105
	// Deficits: 45
	assertDecimalEqual(t, "TotalRolledOver", plan.TotalRolledOver, decimal.NewFromInt(1150))
	assertDecimalEqual(t, "TotalSweptToCategories", plan.TotalSweptToCategories, decimal.NewFromInt(150))
	assertDecimalEqual(t, "TotalSweptToPool", plan.TotalSweptToPool, decimal.NewFromInt(105))
	assertDecimalEqual(t, "TotalDeficit", plan.TotalDeficit, decimal.NewFromInt(45))

	sumCarryovers := decimal.Zero
	for _, r := range plan.Results {
		sumCarryovers = sumCarryovers.Add(r.CarryoverAmount)
	}
	assertDecimalEqual(t, "sum(CarryoverAmount)", sumCarryovers, plan.TotalRolledOver.Add(plan.TotalSweptToCategories))
}
