package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestComputeSpendRecommendations_Unit(t *testing.T) {
	tests := []struct {
		name             string
		planned          string
		m1, m2, m3       string
		wantMedian       string
		wantDeviation    string
		wantShouldPrompt bool
	}{
		{
			name:             "spec case 1: 410, 420, 430 vs 500 planned triggers prompt (16%)",
			planned:          "500.00",
			m1:               "430.00",
			m2:               "420.00",
			m3:               "410.00",
			wantMedian:       "420.00",
			wantDeviation:    "0.1600",
			wantShouldPrompt: true,
		},
		{
			name:             "spec case 2: 480, 490, 500 vs 500 planned does not prompt (2%)",
			planned:          "500.00",
			m1:               "500.00",
			m2:               "490.00",
			m3:               "480.00",
			wantMedian:       "490.00",
			wantDeviation:    "0.0200",
			wantShouldPrompt: false,
		},
		{
			name:             "exact 15% threshold does not prompt",
			planned:          "500.00",
			m1:               "425.00",
			m2:               "425.00",
			m3:               "425.00",
			wantMedian:       "425.00",
			wantDeviation:    "0.1500",
			wantShouldPrompt: false,
		},
		{
			name:             "overspending above 15% prompts upward adjustment",
			planned:          "500.00",
			m1:               "620.00",
			m2:               "600.00",
			m3:               "580.00",
			wantMedian:       "600.00",
			wantDeviation:    "0.2000",
			wantShouldPrompt: true,
		},
		{
			name:             "median resists single-month outlier",
			planned:          "500.00",
			m1:               "1500.00",
			m2:               "500.00",
			m3:               "490.00",
			wantMedian:       "500.00",
			wantDeviation:    "0.0000",
			wantShouldPrompt: false,
		},
		{
			name:             "zero planned skips division by zero and does not prompt",
			planned:          "0.00",
			m1:               "110.00",
			m2:               "120.00",
			m3:               "100.00",
			wantMedian:       "110.00",
			wantDeviation:    "0.0000",
			wantShouldPrompt: false,
		},
		{
			name:             "zero historical spend does not prompt",
			planned:          "500.00",
			m1:               "0.00",
			m2:               "0.00",
			m3:               "0.00",
			wantMedian:       "0.00",
			wantDeviation:    "1.0000",
			wantShouldPrompt: false,
		},
		{
			name:             "one zero month still computes median of 3 months",
			planned:          "500.00",
			m1:               "430.00",
			m2:               "420.00",
			m3:               "0.00",
			wantMedian:       "420.00",
			wantDeviation:    "0.1600",
			wantShouldPrompt: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := EvaluateCategorySpendRecommendation(
				uuid.New(),
				"Test Category",
				decimal.RequireFromString(tc.planned),
				decimal.RequireFromString(tc.m1),
				decimal.RequireFromString(tc.m2),
				decimal.RequireFromString(tc.m3),
			)
			if rec.TrailingMedianSpend.StringFixed(2) != tc.wantMedian {
				t.Fatalf("TrailingMedianSpend = %s, want %s", rec.TrailingMedianSpend.StringFixed(2), tc.wantMedian)
			}
			if rec.PercentageDeviation.StringFixed(4) != tc.wantDeviation {
				t.Fatalf("PercentageDeviation = %s, want %s", rec.PercentageDeviation.StringFixed(4), tc.wantDeviation)
			}
			if rec.ShouldPrompt != tc.wantShouldPrompt {
				t.Fatalf("ShouldPrompt = %v, want %v", rec.ShouldPrompt, tc.wantShouldPrompt)
			}
		})
	}
}

func seedRecommendationTestFixture(t *testing.T, ctx context.Context, tx pgx.Tx, wsID uuid.UUID) (diningID, groceriesID, sinkingID, zeroID uuid.UUID) {
	t.Helper()

	catRepo := NewCategoryRepo()
	baseRepo := NewBaselineRepo()
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	dining := &Category{
		WorkspaceID:   wsID,
		Name:          "Dining",
		GroupName:     "Lifestyle",
		MonthlyBudget: decimal.NewFromInt(500),
		RolloverMode:  RolloverSweep,
		TargetCadence: "NONE",
		SortOrder:     1,
	}
	groceries := &Category{
		WorkspaceID:   wsID,
		Name:          "Groceries",
		GroupName:     "Essentials",
		MonthlyBudget: decimal.NewFromInt(500),
		RolloverMode:  RolloverSweep,
		TargetCadence: "NONE",
		SortOrder:     2,
	}
	sinking := &Category{
		WorkspaceID:   wsID,
		Name:          "Sinking Fund",
		GroupName:     "Savings",
		MonthlyBudget: decimal.NewFromInt(300),
		RolloverMode:  RolloverBalance,
		TargetCadence: "NONE",
		SortOrder:     3,
	}
	zeroCat := &Category{
		WorkspaceID:   wsID,
		Name:          "Zero Budget",
		GroupName:     "Misc",
		MonthlyBudget: decimal.Zero,
		RolloverMode:  RolloverSweep,
		TargetCadence: "NONE",
		SortOrder:     4,
	}

	for _, c := range []*Category{dining, groceries, sinking, zeroCat} {
		if err := catRepo.Create(ctx, tx, c); err != nil {
			t.Fatalf("create category %s: %v", c.Name, err)
		}
	}
	if err := baseRepo.InitializeMonth(ctx, tx, nov); err != nil {
		t.Fatalf("initialize month: %v", err)
	}

	checkingID := uuid.New()
	ccID := uuid.New()
	offBudgetID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, workspace_id, name, type, current_balance, is_on_budget)
		VALUES
			($1, $4, 'Checking', 'checking', 5000.00, TRUE),
			($2, $4, 'Sapphire', 'credit_card', -500.00, TRUE),
			($3, $4, 'Brokerage', 'investment', 10000.00, FALSE)
	`, checkingID, ccID, offBudgetID, wsID); err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	// Linked transfer pair (must be ignored by spend aggregation)
	xferOutID := uuid.New()
	xferInID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (id, workspace_id, account_id, category_id, amount, merchant_raw, transaction_date, status)
		VALUES
			($1, $4, $2, $5, -900.00, 'Transfer Out', '2026-09-12', 'POSTED'),
			($3, $4, $6, NULL, 900.00, 'Transfer In', '2026-09-12', 'POSTED')
	`, xferOutID, checkingID, xferInID, wsID, dining.ID, ccID); err != nil {
		t.Fatalf("insert transfer pair: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET transfer_linked_transaction_id = $2 WHERE id = $1;
	`, xferOutID, xferInID); err != nil {
		t.Fatalf("link transfer out: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET transfer_linked_transaction_id = $2 WHERE id = $1;
	`, xferInID, xferOutID); err != nil {
		t.Fatalf("link transfer in: %v", err)
	}

	// Dining: Aug = 410, Sep = 200 (checking) + 220 (CC) - 50 (rebate) = 370, Oct = 430 => median 410
	// Plus noise that must be ignored: -900 off-budget, -999 in Jul (M-4), -888 in Nov (M)
	// Groceries: Aug = 480, Sep = 490, Oct = 500
	// Zero Budget: Aug/Sep/Oct = 100
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (workspace_id, account_id, category_id, amount, merchant_raw, transaction_date, status)
		VALUES
			($1, $2, $5, -410.00, 'Bistro', '2026-08-10', 'POSTED'),
			($1, $2, $5, -200.00, 'Cafe', '2026-09-05', 'POSTED'),
			($1, $3, $5, -220.00, 'Steakhouse', '2026-09-18', 'PENDING'),
			($1, $3, $5, -430.00, 'Sushi', '2026-10-14', 'POSTED'),
			($1, $2, $5, 50.00, 'Dining Rebate', '2026-09-20', 'POSTED'),
			($1, $4, $5, -900.00, 'Off Budget Dining', '2026-09-21', 'POSTED'),
			($1, $2, $5, -999.00, 'July Outlier', '2026-07-15', 'POSTED'),
			($1, $2, $5, -888.00, 'Current Month Spend', '2026-11-05', 'POSTED'),
			($1, $2, $6, -480.00, 'Market Aug', '2026-08-08', 'POSTED'),
			($1, $2, $6, -490.00, 'Market Sep', '2026-09-08', 'POSTED'),
			($1, $2, $6, -500.00, 'Market Oct', '2026-10-08', 'POSTED'),
			($1, $2, $7, -100.00, 'Misc Aug', '2026-08-03', 'POSTED'),
			($1, $2, $7, -100.00, 'Misc Sep', '2026-09-03', 'POSTED'),
			($1, $2, $7, -100.00, 'Misc Oct', '2026-10-03', 'POSTED')
	`, wsID, checkingID, ccID, offBudgetID, dining.ID, groceries.ID, zeroCat.ID); err != nil {
		t.Fatalf("insert test transactions: %v", err)
	}

	return dining.ID, groceries.ID, sinking.ID, zeroCat.ID
}

func TestComputeSpendRecommendations(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		diningID, groceriesID, sinkingID, zeroID := seedRecommendationTestFixture(t, ctx, tx, wsID)

		recs, err := ComputeSpendRecommendations(ctx, tx, wsID, nov)
		if err != nil {
			return err
		}
		if len(recs) != 4 {
			return fmt.Errorf("got %d recommendations, want 4", len(recs))
		}

		byID := make(map[uuid.UUID]SpendRecommendation, len(recs))
		for _, r := range recs {
			byID[r.CategoryID] = r
		}

		diningRec := byID[diningID]
		if diningRec.TrailingMedianSpend.StringFixed(2) != "410.00" ||
			diningRec.CurrentPlanned.StringFixed(2) != "500.00" ||
			diningRec.PercentageDeviation.StringFixed(4) != "0.1800" ||
			!diningRec.ShouldPrompt {
			return fmt.Errorf("unexpected dining recommendation: %+v", diningRec)
		}

		grocRec := byID[groceriesID]
		if grocRec.TrailingMedianSpend.StringFixed(2) != "490.00" ||
			grocRec.CurrentPlanned.StringFixed(2) != "500.00" ||
			grocRec.PercentageDeviation.StringFixed(4) != "0.0200" ||
			grocRec.ShouldPrompt {
			return fmt.Errorf("unexpected groceries recommendation: %+v", grocRec)
		}

		sinkRec := byID[sinkingID]
		if sinkRec.TrailingMedianSpend.StringFixed(2) != "0.00" || sinkRec.ShouldPrompt {
			return fmt.Errorf("expected dormant sinking fund not to prompt, got %+v", sinkRec)
		}

		zeroRec := byID[zeroID]
		if zeroRec.TrailingMedianSpend.StringFixed(2) != "100.00" ||
			zeroRec.PercentageDeviation.StringFixed(4) != "0.0000" ||
			zeroRec.ShouldPrompt {
			return fmt.Errorf("expected zero-planned category not to prompt, got %+v", zeroRec)
		}

		// Applying recommended target clears ShouldPrompt for Dining
		baseSvc := NewBaselineService(NewBaselineRepo())
		if err := baseSvc.SetMonthlyPlanned(ctx, tx, diningID, nov, decimal.NewFromInt(410)); err != nil {
			return err
		}

		recsAfter, err := ComputeSpendRecommendations(ctx, tx, wsID, nov)
		if err != nil {
			return err
		}
		for _, r := range recsAfter {
			if r.CategoryID == diningID {
				if r.CurrentPlanned.StringFixed(2) != "410.00" ||
					r.PercentageDeviation.StringFixed(4) != "0.0000" ||
					r.ShouldPrompt {
					return fmt.Errorf("expected dining prompt cleared after target update, got %+v", r)
				}
			}
		}

		// Verify year-boundary trailing window (Feb 2026 inspects Nov 2025, Dec 2025, Jan 2026)
		acctID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, $2, 'YearCheck', 'checking', 1000.00, TRUE)
		`, acctID, wsID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, amount, merchant_raw, transaction_date, status)
			VALUES
				($1, $2, $3, -300.00, 'Nov', '2025-11-15', 'POSTED'),
				($1, $2, $3, -310.00, 'Dec', '2025-12-15', 'POSTED'),
				($1, $2, $3, -320.00, 'Jan', '2026-01-15', 'POSTED')
		`, wsID, acctID, groceriesID); err != nil {
			return err
		}

		feb2026 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		febRecs, err := ComputeSpendRecommendations(ctx, tx, wsID, feb2026)
		if err != nil {
			return err
		}
		for _, r := range febRecs {
			if r.CategoryID == groceriesID {
				if r.TrailingMedianSpend.StringFixed(2) != "310.00" || !r.ShouldPrompt {
					return fmt.Errorf("unexpected year-boundary recommendation for groceries: %+v", r)
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("TestComputeSpendRecommendations failed: %v", err)
	}
}

func TestComputeSpendRecommendations_HTTPHandler(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()

	var diningID uuid.UUID
	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		diningID, _, _, _ = seedRecommendationTestFixture(t, ctx, tx, wsID)
		return nil
	})
	if err != nil {
		t.Fatalf("setup HTTP fixture: %v", err)
	}

	router := chi.NewRouter()
	RegisterRoutes(router, appPool)

	doReq := func(method, path string, body string, withAuth bool) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		if withAuth {
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("rejects unauthenticated and invalid params", func(t *testing.T) {
		if rec := doReq(http.MethodGet, "/api/v1/budget/recommendations?month=2026-11", "", false); rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
		}
		if rec := doReq(http.MethodGet, "/api/v1/budget/recommendations?month=bad", "", true); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad month status = %d, want 400", rec.Code)
		}
		if rec := doReq(http.MethodGet, "/api/v1/budget/recommendations?month=2026-11&only_prompt=maybe", "", true); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad only_prompt status = %d, want 400", rec.Code)
		}
	})

	t.Run("lists recommendations, supports only_prompt filter, and clears after target apply", func(t *testing.T) {
		rec := doReq(http.MethodGet, "/api/v1/budget/recommendations?month=2026-11", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET recommendations status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var all []SpendRecommendation
		if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
			t.Fatalf("decode all recommendations: %v", err)
		}
		if len(all) != 4 {
			t.Fatalf("got %d recommendations, want 4", len(all))
		}

		recOnly := doReq(http.MethodGet, "/api/v1/budget/recommendations?month=2026-11&only_prompt=true", "", true)
		if recOnly.Code != http.StatusOK {
			t.Fatalf("GET only_prompt status = %d, body = %s", recOnly.Code, recOnly.Body.String())
		}
		var prompted []SpendRecommendation
		if err := json.Unmarshal(recOnly.Body.Bytes(), &prompted); err != nil {
			t.Fatalf("decode prompted recommendations: %v", err)
		}
		if len(prompted) != 1 || prompted[0].CategoryID != diningID || !prompted[0].ShouldPrompt {
			t.Fatalf("unexpected only_prompt result: %+v", prompted)
		}

		putRec := doReq(
			http.MethodPut,
			"/api/v1/budget/categories/"+diningID.String(),
			`{"year_month":"2026-11","original_planned":410}`,
			true,
		)
		if putRec.Code != http.StatusOK {
			t.Fatalf("PUT monthly planned status = %d, body = %s", putRec.Code, putRec.Body.String())
		}

		recAfter := doReq(http.MethodGet, "/api/v1/budget/recommendations?month=2026-11&only_prompt=true", "", true)
		if recAfter.Code != http.StatusOK {
			t.Fatalf("GET after apply status = %d, body = %s", recAfter.Code, recAfter.Body.String())
		}
		if body := recAfter.Body.String(); body != "[]\n" {
			t.Fatalf("expected empty JSON array after applying target, got %q", body)
		}
	})
}
