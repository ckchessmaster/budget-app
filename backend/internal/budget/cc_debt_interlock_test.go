package budget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestEvaluateCCDebtInterlock(t *testing.T) {
	nov := time.Date(2026, 11, 20, 15, 30, 0, 0, time.UTC)
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	cardA := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	cardB := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	cardC := uuid.MustParse("00000000-0000-0000-0000-00000000000c")

	t.Run("category overspent by $45 on credit card reduces next month Ready to Assign by $45.00", func(t *testing.T) {
		catID := uuid.New()
		plan := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   catID,
				CategoryName: "Dining Out",
				Remaining:    decimal.NewFromFloat(-45.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromFloat(245.00)},
				},
			},
		})

		if !plan.SourceMonth.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("SourceMonth = %v, want 2026-11-01", plan.SourceMonth)
		}
		if !plan.TargetMonth.Equal(dec) {
			t.Fatalf("TargetMonth = %v, want 2026-12-01", plan.TargetMonth)
		}
		if plan.TotalCCDeficit.StringFixed(2) != "45.00" {
			t.Fatalf("TotalCCDeficit = %s, want 45.00", plan.TotalCCDeficit.StringFixed(2))
		}
		if plan.TotalCashDeficit.StringFixed(2) != "0.00" {
			t.Fatalf("TotalCashDeficit = %s, want 0.00", plan.TotalCashDeficit.StringFixed(2))
		}
		if plan.ReadyToAssignDelta.StringFixed(2) != "-45.00" {
			t.Fatalf("ReadyToAssignDelta = %s, want -45.00", plan.ReadyToAssignDelta.StringFixed(2))
		}
		if !plan.RequiresNotification {
			t.Fatalf("expected RequiresNotification = true")
		}
		if len(plan.AccountInterlocks) != 1 || plan.AccountInterlocks[0].AccountID == nil || *plan.AccountInterlocks[0].AccountID != cardA {
			t.Fatalf("unexpected AccountInterlocks: %+v", plan.AccountInterlocks)
		}
		if plan.AccountInterlocks[0].DeficitAmount.StringFixed(2) != "45.00" {
			t.Fatalf("AccountInterlock DeficitAmount = %s, want 45.00", plan.AccountInterlocks[0].DeficitAmount.StringFixed(2))
		}

		nextSummary := aggregateMonthSummary(dec, decimal.NewFromInt(1000), plan.ReadyToAssignDelta, []categoryMonthAmounts{
			{EffectiveTarget: decimal.NewFromInt(200)},
		})
		if nextSummary.ReadyToAssign.StringFixed(2) != "755.00" {
			t.Fatalf("next month ReadyToAssign = %s, want 755.00", nextSummary.ReadyToAssign.StringFixed(2))
		}
	})

	t.Run("zero or surplus remaining produces no interlock and no notification", func(t *testing.T) {
		plan := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Groceries",
				Remaining:    decimal.Zero,
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromInt(100)},
				},
			},
			{
				CategoryID:   uuid.New(),
				CategoryName: "Utilities",
				Remaining:    decimal.NewFromFloat(25.50),
			},
		})

		if plan.TotalCCDeficit.StringFixed(2) != "0.00" || plan.TotalCashDeficit.StringFixed(2) != "0.00" {
			t.Fatalf("expected zero deficits, got cc=%s cash=%s", plan.TotalCCDeficit, plan.TotalCashDeficit)
		}
		if plan.ReadyToAssignDelta.String() != "0" && plan.ReadyToAssignDelta.StringFixed(2) != "0.00" {
			t.Fatalf("expected non-negative zero ReadyToAssignDelta, got %s", plan.ReadyToAssignDelta.StringFixed(2))
		}
		if plan.ReadyToAssignDelta.IsNegative() {
			t.Fatalf("expected ReadyToAssignDelta not to be negative zero, got %s", plan.ReadyToAssignDelta)
		}
		if plan.RequiresNotification {
			t.Fatalf("expected RequiresNotification = false")
		}
		if len(plan.AccountInterlocks) != 0 || len(plan.CategoryDetails) != 0 {
			t.Fatalf("expected empty slices, got interlocks=%d details=%d", len(plan.AccountInterlocks), len(plan.CategoryDetails))
		}
	})

	t.Run("pure cash overspend has zero CC deficit and no notification", func(t *testing.T) {
		plan := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Rent",
				Remaining:    decimal.NewFromFloat(-45.00),
			},
		})

		if plan.TotalCashDeficit.StringFixed(2) != "45.00" {
			t.Fatalf("TotalCashDeficit = %s, want 45.00", plan.TotalCashDeficit.StringFixed(2))
		}
		if plan.TotalCCDeficit.StringFixed(2) != "0.00" {
			t.Fatalf("TotalCCDeficit = %s, want 0.00", plan.TotalCCDeficit.StringFixed(2))
		}
		if plan.ReadyToAssignDelta.StringFixed(2) != "-45.00" {
			t.Fatalf("ReadyToAssignDelta = %s, want -45.00", plan.ReadyToAssignDelta.StringFixed(2))
		}
		if plan.RequiresNotification || len(plan.AccountInterlocks) != 0 {
			t.Fatalf("expected no CC notification or account interlocks, got %+v", plan)
		}
	})

	t.Run("mixed cash and credit card spend prioritizes cash consuming budget first", func(t *testing.T) {
		// Budget = $100, Cash spend = $80, CC spend = $65 => total spend $145, deficit $45 -> all $45 CC deficit.
		plan1 := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Travel",
				Remaining:    decimal.NewFromFloat(-45.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromFloat(65.00)},
				},
			},
		})
		if plan1.TotalCCDeficit.StringFixed(2) != "45.00" || plan1.TotalCashDeficit.StringFixed(2) != "0.00" {
			t.Fatalf("subcase 1: got cc=%s cash=%s, want 45.00 / 0.00", plan1.TotalCCDeficit.StringFixed(2), plan1.TotalCashDeficit.StringFixed(2))
		}

		// Budget = $100, Cash spend = $120, CC spend = $25 => total spend $145, deficit $45 -> $25 CC + $20 Cash.
		plan2 := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Travel",
				Remaining:    decimal.NewFromFloat(-45.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromFloat(25.00)},
				},
			},
		})
		if plan2.TotalCCDeficit.StringFixed(2) != "25.00" || plan2.TotalCashDeficit.StringFixed(2) != "20.00" {
			t.Fatalf("subcase 2: got cc=%s cash=%s, want 25.00 / 20.00", plan2.TotalCCDeficit.StringFixed(2), plan2.TotalCashDeficit.StringFixed(2))
		}
		if plan2.ReadyToAssignDelta.StringFixed(2) != "-45.00" {
			t.Fatalf("subcase 2 ReadyToAssignDelta = %s, want -45.00", plan2.ReadyToAssignDelta.StringFixed(2))
		}
	})

	t.Run("multiple credit cards split deficit proportionally with exact cent conservation", func(t *testing.T) {
		planExact := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Shopping",
				Remaining:    decimal.NewFromFloat(-45.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardB, Amount: decimal.NewFromFloat(30.00)},
					{AccountID: cardA, Amount: decimal.NewFromFloat(60.00)},
				},
			},
		})
		if len(planExact.AccountInterlocks) != 2 {
			t.Fatalf("expected 2 account interlocks, got %d", len(planExact.AccountInterlocks))
		}
		if planExact.AccountInterlocks[0].DeficitAmount.StringFixed(2) != "30.00" ||
			planExact.AccountInterlocks[1].DeficitAmount.StringFixed(2) != "15.00" {
			t.Fatalf("unexpected exact proportional split: %+v", planExact.AccountInterlocks)
		}

		// Equal 3-way split of $10.00 across cards with $10.00 spend each => $3.34, $3.33, $3.33.
		planThreeWay := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Equal Split",
				Remaining:    decimal.NewFromFloat(-10.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardC, Amount: decimal.NewFromFloat(10.00)},
					{AccountID: cardB, Amount: decimal.NewFromFloat(10.00)},
					{AccountID: cardA, Amount: decimal.NewFromFloat(10.00)},
				},
			},
		})
		sum := decimal.Zero
		for _, il := range planThreeWay.AccountInterlocks {
			sum = sum.Add(il.DeficitAmount)
		}
		if sum.StringFixed(2) != "10.00" {
			t.Fatalf("3-way sum = %s, want 10.00", sum.StringFixed(2))
		}
		if planThreeWay.AccountInterlocks[0].DeficitAmount.StringFixed(2) != "3.34" ||
			planThreeWay.AccountInterlocks[1].DeficitAmount.StringFixed(2) != "3.33" ||
			planThreeWay.AccountInterlocks[2].DeficitAmount.StringFixed(2) != "3.33" {
			t.Fatalf("unexpected 3-way split: %+v", planThreeWay.AccountInterlocks)
		}
	})

	t.Run("multiple overspent categories aggregate onto the same credit card", func(t *testing.T) {
		plan := EvaluateCCDebtInterlock(nov, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Dining",
				Remaining:    decimal.NewFromFloat(-25.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromFloat(50.00)},
				},
			},
			{
				CategoryID:   uuid.New(),
				CategoryName: "Coffee",
				Remaining:    decimal.NewFromFloat(-20.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromFloat(20.00)},
				},
			},
		})
		if len(plan.AccountInterlocks) != 1 || plan.AccountInterlocks[0].DeficitAmount.StringFixed(2) != "45.00" {
			t.Fatalf("expected single aggregated interlock of 45.00, got %+v", plan.AccountInterlocks)
		}
		if plan.TotalCCDeficit.StringFixed(2) != "45.00" {
			t.Fatalf("TotalCCDeficit = %s, want 45.00", plan.TotalCCDeficit.StringFixed(2))
		}
	})

	t.Run("ignores non-positive CC spend and handles year boundary rollover", func(t *testing.T) {
		decLate := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
		plan := EvaluateCCDebtInterlock(decLate, []CategoryDeficitInput{
			{
				CategoryID:   uuid.New(),
				CategoryName: "Gifts",
				Remaining:    decimal.NewFromFloat(-15.00),
				CCSpendByCard: []CCAccountSpend{
					{AccountID: cardA, Amount: decimal.NewFromFloat(-20.00)},
					{AccountID: cardB, Amount: decimal.Zero},
				},
			},
		})
		if !plan.TargetMonth.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("TargetMonth = %v, want 2027-01-01", plan.TargetMonth)
		}
		if plan.TotalCashDeficit.StringFixed(2) != "15.00" || plan.TotalCCDeficit.StringFixed(2) != "0.00" {
			t.Fatalf("expected pure cash deficit 15.00, got cc=%s cash=%s", plan.TotalCCDeficit, plan.TotalCashDeficit)
		}
	})
}

func setupCCInterlockTestFixture(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	wsID uuid.UUID,
	initialDebtBaseline decimal.Decimal,
	overspendAmount decimal.Decimal,
) (uuid.UUID, uuid.UUID, CCDebtInterlockPlan, CreditCardDebtInterlock) {
	t.Helper()
	catRepo := NewCategoryRepo()
	incomeRepo := NewPlannedIncomeRepo()
	baseRepo := NewBaselineRepo()
	svc := NewCCDebtInterlockService()

	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	for _, ym := range []time.Time{nov, dec} {
		if err := incomeRepo.Create(ctx, tx, &MonthlyPlannedIncome{
			WorkspaceID: wsID,
			YearMonth:   ym,
			Name:        "Salary",
			Amount:      decimal.NewFromInt(1000),
		}); err != nil {
			t.Fatalf("create income: %v", err)
		}
	}

	cat := &Category{
		WorkspaceID:   wsID,
		Name:          "Dining Out",
		GroupName:     "Everyday",
		MonthlyBudget: decimal.NewFromInt(200),
		RolloverMode:  RolloverSweep,
		TargetCadence: "NONE",
	}
	if err := catRepo.Create(ctx, tx, cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := baseRepo.InitializeMonth(ctx, tx, nov); err != nil {
		t.Fatalf("init nov baseline: %v", err)
	}
	if err := baseRepo.InitializeMonth(ctx, tx, dec); err != nil {
		t.Fatalf("init dec baseline: %v", err)
	}

	checkingID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, workspace_id, name, type, current_balance, is_on_budget)
		VALUES ($1, $2, 'Primary Checking', 'checking', 3000.00, TRUE)
	`, checkingID, wsID); err != nil {
		t.Fatalf("insert checking account: %v", err)
	}

	cardID := uuid.New()
	totalCharge := decimal.NewFromInt(200).Add(overspendAmount)
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, workspace_id, name, type, current_balance, is_on_budget, card_mode, initial_debt_baseline)
		VALUES ($1, $2, 'Sapphire Reserve', 'credit_card', $3, TRUE, 'PAY_IN_FULL', $4)
	`, cardID, wsID, totalCharge.Neg(), initialDebtBaseline); err != nil {
		t.Fatalf("insert credit card account: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (workspace_id, account_id, category_id, amount, merchant_raw, transaction_date, status)
		VALUES ($1, $2, $3, $4, 'Bistro', '2026-11-18', 'POSTED')
	`, wsID, cardID, cat.ID, totalCharge.Neg()); err != nil {
		t.Fatalf("insert cc transaction: %v", err)
	}

	novDetails, err := ComputeMonthDetails(ctx, tx, nov)
	if err != nil {
		t.Fatalf("ComputeMonthDetails(nov): %v", err)
	}
	ccByCat, err := svc.LoadCategoryCCSpendByMonth(ctx, tx, nov)
	if err != nil {
		t.Fatalf("LoadCategoryCCSpendByMonth: %v", err)
	}

	inputs := make([]CategoryDeficitInput, 0, len(novDetails.Categories))
	for _, c := range novDetails.Categories {
		inputs = append(inputs, CategoryDeficitInput{
			CategoryID:    c.ID,
			CategoryName:  c.Name,
			Remaining:     c.Remaining,
			CCSpendByCard: ccByCat[c.ID],
		})
	}
	plan := EvaluateCCDebtInterlock(nov, inputs)
	persisted, err := svc.PersistInterlockPlan(ctx, tx, plan)
	if err != nil {
		t.Fatalf("PersistInterlockPlan: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted interlock, got %d", len(persisted))
	}

	return cardID, cat.ID, plan, persisted[0]
}

func TestCCDebtInterlockPersistAndMonthSummary(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()
	svc := NewCCDebtInterlockService()
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		cardID, _, plan, il := setupCCInterlockTestFixture(
			t, ctx, tx, wsID, decimal.Zero, decimal.NewFromFloat(45.00),
		)

		if il.DeficitAmount.StringFixed(2) != "45.00" || il.Status != CCInterlockPending {
			t.Fatalf("unexpected persisted interlock: %+v", il)
		}

		// Calling PersistInterlockPlan again for the same month must be idempotent.
		persisted2, err := svc.PersistInterlockPlan(ctx, tx, plan)
		if err != nil {
			return err
		}
		if len(persisted2) != 1 || persisted2[0].ID != il.ID {
			t.Fatalf("expected idempotent interlock return %s, got %+v", il.ID, persisted2)
		}

		var adjCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM monthly_rollover_adjustments
			WHERE workspace_id = $1 AND interlock_id = $2
		`, wsID, il.ID).Scan(&adjCount); err != nil {
			return err
		}
		if adjCount != 1 {
			t.Fatalf("expected 1 rollover adjustment row, got %d", adjCount)
		}

		decDetails, err := ComputeMonthDetails(ctx, tx, dec)
		if err != nil {
			return err
		}
		if decDetails.Summary.RolloverAdjustment.StringFixed(2) != "-45.00" {
			t.Fatalf("Dec RolloverAdjustment = %s, want -45.00", decDetails.Summary.RolloverAdjustment.StringFixed(2))
		}
		if decDetails.Summary.ReadyToAssign.StringFixed(2) != "755.00" {
			t.Fatalf("Dec ReadyToAssign = %s, want 755.00", decDetails.Summary.ReadyToAssign.StringFixed(2))
		}
		if len(decDetails.PendingInterlocks) != 1 {
			t.Fatalf("expected 1 PendingInterlock in Dec details, got %d", len(decDetails.PendingInterlocks))
		}
		pi := decDetails.PendingInterlocks[0]
		if pi.ID != il.ID || pi.AccountID == nil || *pi.AccountID != cardID || pi.AccountName != "Sapphire Reserve" {
			t.Fatalf("unexpected PendingInterlock: %+v", pi)
		}
		if pi.DeficitAmount.StringFixed(2) != "45.00" {
			t.Fatalf("PendingInterlock DeficitAmount = %s, want 45.00", pi.DeficitAmount.StringFixed(2))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestCCDebtInterlockPersistAndMonthSummary failed: %v", err)
	}
}

func TestCCDebtInterlockResolveKeepDeduction(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()
	svc := NewCCDebtInterlockService()
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		cardID, _, _, il := setupCCInterlockTestFixture(
			t, ctx, tx, wsID, decimal.NewFromFloat(100.00), decimal.NewFromFloat(45.00),
		)

		resp, err := svc.ResolveInterlock(ctx, tx, il.ID, CCInterlockActionKeepDeduction)
		if err != nil {
			return err
		}
		if resp.Interlock.Status != CCInterlockKeepDeduction || resp.Interlock.ResolvedAt == nil {
			t.Fatalf("unexpected resolved interlock: %+v", resp.Interlock)
		}
		if resp.ReadyToAssignRestored.StringFixed(2) != "0.00" {
			t.Fatalf("ReadyToAssignRestored = %s, want 0.00", resp.ReadyToAssignRestored.StringFixed(2))
		}
		if resp.UpdatedInitialDebtBaseline != nil {
			t.Fatalf("expected nil UpdatedInitialDebtBaseline, got %v", resp.UpdatedInitialDebtBaseline)
		}

		var baseline decimal.Decimal
		if err := tx.QueryRow(ctx, `SELECT initial_debt_baseline FROM accounts WHERE id = $1`, cardID).Scan(&baseline); err != nil {
			return err
		}
		if baseline.StringFixed(2) != "100.00" {
			t.Fatalf("initial_debt_baseline changed to %s, want 100.00", baseline.StringFixed(2))
		}

		decDetails, err := ComputeMonthDetails(ctx, tx, dec)
		if err != nil {
			return err
		}
		if decDetails.Summary.RolloverAdjustment.StringFixed(2) != "-45.00" {
			t.Fatalf("Dec RolloverAdjustment = %s, want -45.00", decDetails.Summary.RolloverAdjustment.StringFixed(2))
		}
		if decDetails.Summary.ReadyToAssign.StringFixed(2) != "755.00" {
			t.Fatalf("Dec ReadyToAssign = %s, want 755.00", decDetails.Summary.ReadyToAssign.StringFixed(2))
		}
		if len(decDetails.PendingInterlocks) != 0 {
			t.Fatalf("expected 0 PendingInterlocks after resolution, got %d", len(decDetails.PendingInterlocks))
		}

		if _, err := svc.ResolveInterlock(ctx, tx, il.ID, CCInterlockActionConvertToDebt); !errors.Is(err, ErrInterlockAlreadyResolved) {
			t.Fatalf("expected ErrInterlockAlreadyResolved on repeat resolve, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestCCDebtInterlockResolveKeepDeduction failed: %v", err)
	}
}

func TestCCDebtInterlockResolveConvertToDebt(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()
	svc := NewCCDebtInterlockService()
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		cardID, _, _, il := setupCCInterlockTestFixture(
			t, ctx, tx, wsID, decimal.NewFromFloat(100.00), decimal.NewFromFloat(45.00),
		)

		resp, err := svc.ResolveInterlock(ctx, tx, il.ID, CCInterlockActionConvertToDebt)
		if err != nil {
			return err
		}
		if resp.Interlock.Status != CCInterlockConvertToDebt || resp.Interlock.ResolvedAt == nil {
			t.Fatalf("unexpected resolved interlock: %+v", resp.Interlock)
		}
		if resp.ReadyToAssignRestored.StringFixed(2) != "45.00" {
			t.Fatalf("ReadyToAssignRestored = %s, want 45.00", resp.ReadyToAssignRestored.StringFixed(2))
		}
		if resp.UpdatedInitialDebtBaseline == nil || resp.UpdatedInitialDebtBaseline.StringFixed(2) != "145.00" {
			t.Fatalf("UpdatedInitialDebtBaseline = %v, want 145.00", resp.UpdatedInitialDebtBaseline)
		}

		var baseline decimal.Decimal
		if err := tx.QueryRow(ctx, `SELECT initial_debt_baseline FROM accounts WHERE id = $1`, cardID).Scan(&baseline); err != nil {
			return err
		}
		if baseline.StringFixed(2) != "145.00" {
			t.Fatalf("DB initial_debt_baseline = %s, want 145.00", baseline.StringFixed(2))
		}

		decDetails, err := ComputeMonthDetails(ctx, tx, dec)
		if err != nil {
			return err
		}
		if decDetails.Summary.RolloverAdjustment.StringFixed(2) != "0.00" {
			t.Fatalf("Dec RolloverAdjustment = %s, want 0.00", decDetails.Summary.RolloverAdjustment.StringFixed(2))
		}
		if decDetails.Summary.ReadyToAssign.StringFixed(2) != "800.00" {
			t.Fatalf("Dec ReadyToAssign = %s, want 800.00", decDetails.Summary.ReadyToAssign.StringFixed(2))
		}
		if len(decDetails.PendingInterlocks) != 0 {
			t.Fatalf("expected 0 PendingInterlocks after resolution, got %d", len(decDetails.PendingInterlocks))
		}

		if _, err := svc.ResolveInterlock(ctx, tx, il.ID, CCInterlockActionConvertToDebt); !errors.Is(err, ErrInterlockAlreadyResolved) {
			t.Fatalf("expected ErrInterlockAlreadyResolved on repeat resolve, got %v", err)
		}

		// Confirm baseline was not double-incremented.
		if err := tx.QueryRow(ctx, `SELECT initial_debt_baseline FROM accounts WHERE id = $1`, cardID).Scan(&baseline); err != nil {
			return err
		}
		if baseline.StringFixed(2) != "145.00" {
			t.Fatalf("DB initial_debt_baseline after repeat = %s, want 145.00", baseline.StringFixed(2))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestCCDebtInterlockResolveConvertToDebt failed: %v", err)
	}
}

func TestCCDebtInterlockHandlersHTTP(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()

	var interlockID uuid.UUID
	var orphanInterlockID uuid.UUID

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, _, _, il := setupCCInterlockTestFixture(
			t, ctx, tx, wsID, decimal.Zero, decimal.NewFromFloat(45.00),
		)
		interlockID = il.ID

		// Also insert an interlock with NULL account_id (e.g. account deleted after month-end) for edge-case validation.
		svc := NewCCDebtInterlockService()
		oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		orphanPlan := CCDebtInterlockPlan{
			SourceMonth: oct,
			TargetMonth: nov,
			AccountInterlocks: []CCAccountInterlock{
				{AccountID: nil, DeficitAmount: decimal.NewFromFloat(12.50)},
			},
		}
		persisted, err := svc.PersistInterlockPlan(ctx, tx, orphanPlan)
		if err != nil {
			return err
		}
		orphanInterlockID = persisted[0].ID
		return nil
	})
	if err != nil {
		t.Fatalf("setup HTTP fixture: %v", err)
	}

	router := chi.NewRouter()
	RegisterRoutes(router, appPool)

	doReq := func(method, path string, body any, withAuth bool) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		if withAuth {
			ctxWithUser := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			ctxWithWS := middleware.WithWorkspaceContext(ctxWithUser, middleware.WorkspaceContext{
				WorkspaceID: wsID,
				UserID:      userID,
				Role:        middleware.RoleOwner,
			})
			req = req.WithContext(db.WithWorkspaceID(ctxWithWS, wsID))
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("unauthenticated requests return 401", func(t *testing.T) {
		if rec := doReq(http.MethodGet, "/api/v1/budget/cc-interlocks", nil, false); rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET status = %d, want 401", rec.Code)
		}
		if rec := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/"+interlockID.String()+"/resolve", map[string]string{"action": "KEEP_DEDUCTION"}, false); rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST status = %d, want 401", rec.Code)
		}
	})

	t.Run("list validates month and status query params and filters results", func(t *testing.T) {
		if rec := doReq(http.MethodGet, "/api/v1/budget/cc-interlocks?month=bad", nil, true); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad month status = %d, want 400", rec.Code)
		}
		if rec := doReq(http.MethodGet, "/api/v1/budget/cc-interlocks?status=UNKNOWN", nil, true); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad status param = %d, want 400", rec.Code)
		}

		rec := doReq(http.MethodGet, "/api/v1/budget/cc-interlocks?month=2026-12&status=PENDING", nil, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("filtered GET status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var items []CCDebtInterlockView
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		if len(items) != 1 || items[0].ID != interlockID || items[0].DeficitAmount.StringFixed(2) != "45.00" {
			t.Fatalf("unexpected filtered items: %+v", items)
		}

		recAll := doReq(http.MethodGet, "/api/v1/budget/cc-interlocks", nil, true)
		if recAll.Code != http.StatusOK {
			t.Fatalf("unfiltered GET status = %d", recAll.Code)
		}
		var allItems []CCDebtInterlockView
		if err := json.Unmarshal(recAll.Body.Bytes(), &allItems); err != nil {
			t.Fatalf("decode unfiltered list response: %v", err)
		}
		if len(allItems) != 2 {
			t.Fatalf("expected 2 total interlocks, got %d", len(allItems))
		}
	})

	t.Run("resolve endpoint validates input, handles errors, and resolves interlock", func(t *testing.T) {
		if rec := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/not-a-uuid/resolve", map[string]string{"action": "KEEP_DEDUCTION"}, true); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad uuid status = %d, want 400", rec.Code)
		}
		if rec := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/"+interlockID.String()+"/resolve", map[string]string{"action": "INVALID"}, true); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad action status = %d, want 400", rec.Code)
		}
		if rec := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/"+uuid.New().String()+"/resolve", map[string]string{"action": "KEEP_DEDUCTION"}, true); rec.Code != http.StatusNotFound {
			t.Fatalf("missing id status = %d, want 404", rec.Code)
		}
		if rec := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/"+orphanInterlockID.String()+"/resolve", map[string]string{"action": "CONVERT_TO_DEBT"}, true); rec.Code != http.StatusBadRequest {
			t.Fatalf("orphan CONVERT_TO_DEBT status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
		}

		rec := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/"+interlockID.String()+"/resolve", map[string]string{"action": "CONVERT_TO_DEBT"}, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("resolve CONVERT_TO_DEBT status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp ResolveCCDebtInterlockResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode resolve response: %v", err)
		}
		if resp.Interlock.Status != CCInterlockConvertToDebt || resp.ReadyToAssignRestored.StringFixed(2) != "45.00" {
			t.Fatalf("unexpected resolve response: %+v", resp)
		}
		if resp.UpdatedInitialDebtBaseline == nil || resp.UpdatedInitialDebtBaseline.StringFixed(2) != "45.00" {
			t.Fatalf("unexpected updated baseline: %+v", resp.UpdatedInitialDebtBaseline)
		}

		if recDup := doReq(http.MethodPost, "/api/v1/budget/cc-interlocks/"+interlockID.String()+"/resolve", map[string]string{"action": "KEEP_DEDUCTION"}, true); recDup.Code != http.StatusConflict {
			t.Fatalf("duplicate resolve status = %d, want 409", recDup.Code)
		}
	})
}
