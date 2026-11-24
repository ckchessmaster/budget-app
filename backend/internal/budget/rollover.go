package budget

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RolloverExecutionResult captures the month-end rollover calculation for a single category.
// NextMonthAssigned maps to monthly_category_budgets.original_planned in M+1.
// CarryoverAmount maps to monthly_category_budgets.carryover_balance in M+1.
// NextMonthAvailable equals CarryoverAmount + NextMonthAssigned.
type RolloverExecutionResult struct {
	CategoryID         uuid.UUID       `json:"category_id"`
	Mode               RolloverMode    `json:"mode"`
	PriorRemaining     decimal.Decimal `json:"prior_remaining"`
	CarryoverAmount    decimal.Decimal `json:"carryover_amount"`
	NextMonthAssigned  decimal.Decimal `json:"next_month_assigned"`
	NextMonthAvailable decimal.Decimal `json:"next_month_available"`
	SweptAmount        decimal.Decimal `json:"swept_amount"`
	SweepTargetID      *uuid.UUID      `json:"sweep_target_id,omitempty"`
	SweptToPool        decimal.Decimal `json:"swept_to_pool"`
	DeficitAmount      decimal.Decimal `json:"deficit_amount"`
}

// CategoryRolloverInput pairs a category definition with its month-end remaining balance.
// Callers using a frozen month M baseline should populate Category.MonthlyBudget with that baseline.
type CategoryRolloverInput struct {
	Category  Category
	Remaining decimal.Decimal
}

// SweepTransfer represents a directed month-end sweep from one category to another.
type SweepTransfer struct {
	FromCategoryID uuid.UUID       `json:"from_category_id"`
	ToCategoryID   uuid.UUID       `json:"to_category_id"`
	Amount         decimal.Decimal `json:"amount"`
}

// WorkspaceRolloverPlan aggregates per-category rollover outputs and cross-category sweep routing
// for a workspace at a month boundary.
// TotalRolledOver sums direct category carryovers (before inbound sweeps), so
// TotalRolledOver + TotalSweptToCategories + TotalSweptToPool == total positive surplus across categories,
// and sum(Results[i].CarryoverAmount) == TotalRolledOver + TotalSweptToCategories.
type WorkspaceRolloverPlan struct {
	Results                []RolloverExecutionResult `json:"results"`
	SweepTransfers         []SweepTransfer           `json:"sweep_transfers"`
	TotalRolledOver        decimal.Decimal           `json:"total_rolled_over"`
	TotalSweptToCategories decimal.Decimal           `json:"total_swept_to_categories"`
	TotalSweptToPool       decimal.Decimal           `json:"total_swept_to_pool"`
	TotalDeficit           decimal.Decimal           `json:"total_deficit"`
}

func roundMoney(d decimal.Decimal) decimal.Decimal {
	r := d.Round(2)
	if r.IsZero() {
		return decimal.Zero.Round(2)
	}
	return r
}

func resolveTargetCap(cat Category, fallbackPlanned decimal.Decimal) decimal.Decimal {
	capAmount := fallbackPlanned
	if cat.TargetAmount != nil {
		capAmount = roundMoney(*cat.TargetAmount)
	}
	if capAmount.LessThan(decimal.Zero) {
		return decimal.Zero.Round(2)
	}
	return capAmount
}

func normalizeRolloverMode(mode RolloverMode) RolloverMode {
	switch mode {
	case RolloverSweep, RolloverBalance, RolloverTarget:
		return mode
	default:
		return RolloverSweep
	}
}

// EvaluateCategoryRollover computes the month-end rollover disposition for a single category
// given its end-of-month remaining balance.
func EvaluateCategoryRollover(cat Category, remaining decimal.Decimal) RolloverExecutionResult {
	zero := decimal.Zero.Round(2)
	rem := roundMoney(remaining)
	planned := roundMoney(cat.MonthlyBudget)
	if planned.LessThan(decimal.Zero) {
		planned = zero
	}
	mode := normalizeRolloverMode(cat.RolloverMode)

	res := RolloverExecutionResult{
		CategoryID:         cat.ID,
		Mode:               mode,
		PriorRemaining:     rem,
		CarryoverAmount:    zero,
		NextMonthAssigned:  zero,
		NextMonthAvailable: zero,
		SweptAmount:        zero,
		SweptToPool:        zero,
		DeficitAmount:      zero,
	}

	if rem.LessThan(decimal.Zero) {
		// Deficits are deducted from next month's Ready to Assign rather than carried into the category.
		res.DeficitAmount = roundMoney(rem.Abs())
		res.CarryoverAmount = zero
		if mode == RolloverTarget {
			res.NextMonthAssigned = resolveTargetCap(cat, planned)
		} else {
			res.NextMonthAssigned = planned
		}
		res.NextMonthAvailable = res.NextMonthAssigned
		return res
	}

	switch mode {
	case RolloverBalance:
		res.CarryoverAmount = rem
		res.NextMonthAssigned = planned
		res.NextMonthAvailable = roundMoney(rem.Add(planned))
	case RolloverTarget:
		capAmount := resolveTargetCap(cat, planned)
		needed := roundMoney(capAmount.Sub(rem))
		if needed.LessThan(decimal.Zero) {
			needed = zero
		}
		res.CarryoverAmount = rem
		res.NextMonthAssigned = needed
		res.NextMonthAvailable = roundMoney(rem.Add(needed))
	case RolloverSweep:
		res.CarryoverAmount = zero
		res.NextMonthAssigned = planned
		res.NextMonthAvailable = planned
		if rem.GreaterThan(decimal.Zero) {
			res.SweptAmount = rem
			if cat.SweepTargetCategoryID != nil && *cat.SweepTargetCategoryID != uuid.Nil && *cat.SweepTargetCategoryID != cat.ID {
				targetID := *cat.SweepTargetCategoryID
				res.SweepTargetID = &targetID
				res.SweptToPool = zero
			} else {
				res.SweepTargetID = nil
				res.SweptToPool = rem
			}
		}
	}

	return res
}

// PlanWorkspaceRollover evaluates rollover rules across all categories in a workspace,
// routes category-to-category sweeps, falls back dangling sweep targets to Ready to Assign,
// and adjusts target-cap assignments for inbound swept funds.
func PlanWorkspaceRollover(inputs []CategoryRolloverInput) WorkspaceRolloverPlan {
	zero := decimal.Zero.Round(2)
	plan := WorkspaceRolloverPlan{
		Results:                make([]RolloverExecutionResult, len(inputs)),
		SweepTransfers:         make([]SweepTransfer, 0),
		TotalRolledOver:        zero,
		TotalSweptToCategories: zero,
		TotalSweptToPool:       zero,
		TotalDeficit:           zero,
	}

	idxByID := make(map[uuid.UUID]int, len(inputs))
	for i, in := range inputs {
		idxByID[in.Category.ID] = i
	}

	for i, in := range inputs {
		res := EvaluateCategoryRollover(in.Category, in.Remaining)
		plan.Results[i] = res
		plan.TotalRolledOver = roundMoney(plan.TotalRolledOver.Add(res.CarryoverAmount))
		plan.TotalDeficit = roundMoney(plan.TotalDeficit.Add(res.DeficitAmount))
	}

	inboundSweeps := make(map[uuid.UUID]decimal.Decimal)
	for i := range plan.Results {
		res := &plan.Results[i]
		if !res.SweptAmount.GreaterThan(decimal.Zero) {
			continue
		}
		if res.SweepTargetID != nil {
			if _, exists := idxByID[*res.SweepTargetID]; exists && *res.SweepTargetID != res.CategoryID {
				plan.SweepTransfers = append(plan.SweepTransfers, SweepTransfer{
					FromCategoryID: res.CategoryID,
					ToCategoryID:   *res.SweepTargetID,
					Amount:         res.SweptAmount,
				})
				inboundSweeps[*res.SweepTargetID] = roundMoney(inboundSweeps[*res.SweepTargetID].Add(res.SweptAmount))
				plan.TotalSweptToCategories = roundMoney(plan.TotalSweptToCategories.Add(res.SweptAmount))
				continue
			}
			// Target category does not exist in workspace; fall back to assignable pool.
			res.SweepTargetID = nil
			res.SweptToPool = res.SweptAmount
		}
		plan.TotalSweptToPool = roundMoney(plan.TotalSweptToPool.Add(res.SweptToPool))
	}

	for i := range plan.Results {
		res := &plan.Results[i]
		inbound, ok := inboundSweeps[res.CategoryID]
		if !ok || !inbound.GreaterThan(decimal.Zero) {
			continue
		}
		res.CarryoverAmount = roundMoney(res.CarryoverAmount.Add(inbound))
		if res.Mode == RolloverTarget {
			planned := roundMoney(inputs[i].Category.MonthlyBudget)
			if planned.LessThan(decimal.Zero) {
				planned = zero
			}
			capAmount := resolveTargetCap(inputs[i].Category, planned)
			needed := roundMoney(capAmount.Sub(res.CarryoverAmount))
			if needed.LessThan(decimal.Zero) {
				needed = zero
			}
			res.NextMonthAssigned = needed
		}
		res.NextMonthAvailable = roundMoney(res.CarryoverAmount.Add(res.NextMonthAssigned))
	}

	return plan
}
