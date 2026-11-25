package budget

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// SpendRecommendation captures a trailing 3-month spend trend comparison against a category's planned budget.
type SpendRecommendation struct {
	CategoryID          uuid.UUID       `json:"category_id"`
	CategoryName        string          `json:"category_name"`
	CurrentPlanned      decimal.Decimal `json:"current_planned"`
	TrailingMedianSpend decimal.Decimal `json:"trailing_median_spend"`
	PercentageDeviation decimal.Decimal `json:"percentage_deviation"`
	ShouldPrompt        bool            `json:"should_prompt"`
}

var recommendationDeviationThreshold = decimal.RequireFromString("0.15")

func medianOfThree(a, b, c decimal.Decimal) decimal.Decimal {
	vals := []decimal.Decimal{roundMoney(a), roundMoney(b), roundMoney(c)}
	sort.Slice(vals, func(i, j int) bool {
		return vals[i].Cmp(vals[j]) < 0
	})
	return roundMoney(vals[1])
}

// EvaluateCategorySpendRecommendation compares trailing 3-month median spend against currentPlanned.
func EvaluateCategorySpendRecommendation(
	categoryID uuid.UUID,
	categoryName string,
	currentPlanned, m1Spend, m2Spend, m3Spend decimal.Decimal,
) SpendRecommendation {
	planned := roundMoney(currentPlanned)
	median := roundMoney(medianOfThree(m1Spend, m2Spend, m3Spend))
	pctDeviation := decimal.Zero
	shouldPrompt := false

	if planned.GreaterThan(decimal.Zero) {
		rawDeviation := median.Sub(planned).Abs().Div(planned)
		pctDeviation = rawDeviation.Round(4)
		// Require positive trailing median spend so brand-new categories or dormant sinking funds don't prompt $0.
		if median.GreaterThan(decimal.Zero) && rawDeviation.GreaterThan(recommendationDeviationThreshold) {
			shouldPrompt = true
		}
	}

	return SpendRecommendation{
		CategoryID:          categoryID,
		CategoryName:        categoryName,
		CurrentPlanned:      planned,
		TrailingMedianSpend: median,
		PercentageDeviation: pctDeviation,
		ShouldPrompt:        shouldPrompt,
	}
}

// ComputeSpendRecommendations evaluates trailing 3-month median spend trends for every category in workspaceID for yearMonth.
func ComputeSpendRecommendations(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID uuid.UUID,
	yearMonth time.Time,
) ([]SpendRecommendation, error) {
	if workspaceID == uuid.Nil {
		var err error
		workspaceID, err = db.ActiveWorkspaceID(ctx, tx)
		if err != nil {
			return nil, err
		}
	}

	ym := NormalizeYearMonth(yearMonth)
	m1 := ym.AddDate(0, -1, 0)
	m2 := ym.AddDate(0, -2, 0)
	m3 := ym.AddDate(0, -3, 0)

	type categoryPlannedRow struct {
		id      uuid.UUID
		name    string
		planned decimal.Decimal
	}

	catRows, err := tx.Query(ctx, `
		SELECT c.id,
		       c.name,
		       COALESCE(mcb.original_planned, c.monthly_budget) AS current_planned
		FROM categories c
		LEFT JOIN monthly_category_budgets mcb
		       ON mcb.workspace_id = $1
		      AND mcb.category_id = c.id
		      AND mcb.year_month = $2::date
		WHERE c.workspace_id = $1
		ORDER BY c.sort_order ASC, c.group_name ASC, c.name ASC
	`, workspaceID, ym)
	if err != nil {
		return nil, fmt.Errorf("failed to load category planned budgets for recommendations: %w", err)
	}
	defer catRows.Close()

	cats := make([]categoryPlannedRow, 0)
	for catRows.Next() {
		var row categoryPlannedRow
		if err := catRows.Scan(&row.id, &row.name, &row.planned); err != nil {
			return nil, fmt.Errorf("failed to scan category planned budget: %w", err)
		}
		cats = append(cats, row)
	}
	if err := catRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating category planned budgets: %w", err)
	}

	spendRows, err := tx.Query(ctx, `
		SELECT t.category_id,
		       date_trunc('month', t.transaction_date)::date AS spend_month,
		       GREATEST(0, COALESCE(SUM(-t.amount), 0)) AS total_spent
		FROM transactions t
		INNER JOIN accounts a ON a.id = t.account_id
		WHERE t.workspace_id = $1
		  AND a.workspace_id = $1
		  AND t.category_id IS NOT NULL
		  AND t.is_transfer = FALSE
		  AND a.is_on_budget = TRUE
		  AND t.transaction_date >= $2::date
		  AND t.transaction_date < $3::date
		GROUP BY t.category_id, date_trunc('month', t.transaction_date)::date
	`, workspaceID, m3, ym)
	if err != nil {
		return nil, fmt.Errorf("failed to load trailing monthly spend for recommendations: %w", err)
	}
	defer spendRows.Close()

	monthlySpendByCat := make(map[uuid.UUID][3]decimal.Decimal)
	for spendRows.Next() {
		var catID uuid.UUID
		var spendMonth time.Time
		var totalSpent decimal.Decimal
		if err := spendRows.Scan(&catID, &spendMonth, &totalSpent); err != nil {
			return nil, fmt.Errorf("failed to scan trailing monthly spend: %w", err)
		}

		normMonth := NormalizeYearMonth(spendMonth)
		bucket := monthlySpendByCat[catID]
		switch {
		case normMonth.Equal(m1):
			bucket[0] = totalSpent
		case normMonth.Equal(m2):
			bucket[1] = totalSpent
		case normMonth.Equal(m3):
			bucket[2] = totalSpent
		}
		monthlySpendByCat[catID] = bucket
	}
	if err := spendRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating trailing monthly spend: %w", err)
	}

	recs := make([]SpendRecommendation, 0, len(cats))
	for _, cat := range cats {
		spends := monthlySpendByCat[cat.id]
		recs = append(recs, EvaluateCategorySpendRecommendation(
			cat.id,
			cat.name,
			cat.planned,
			spends[0],
			spends[1],
			spends[2],
		))
	}
	return recs, nil
}

// GetSpendRecommendations handles GET /api/v1/budget/recommendations.
func (h *Handler) GetSpendRecommendations(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ym, err := parseMonthParam(r.URL.Query().Get("month"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid month format, expected YYYY-MM")
		return
	}

	onlyPrompt := false
	if rawOnlyPrompt := strings.TrimSpace(r.URL.Query().Get("only_prompt")); rawOnlyPrompt != "" {
		parsed, parseErr := strconv.ParseBool(rawOnlyPrompt)
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, "invalid only_prompt flag, expected true or false")
			return
		}
		onlyPrompt = parsed
	}

	var recs []SpendRecommendation
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		wsID, wsErr := db.ActiveWorkspaceID(r.Context(), tx)
		if wsErr != nil {
			return wsErr
		}
		var txErr error
		recs, txErr = ComputeSpendRecommendations(r.Context(), tx, wsID, ym)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_compute_spend_recommendations")
		return
	}

	if onlyPrompt {
		filtered := make([]SpendRecommendation, 0, len(recs))
		for _, rec := range recs {
			if rec.ShouldPrompt {
				filtered = append(filtered, rec)
			}
		}
		recs = filtered
	}

	writeJSON(w, http.StatusOK, recs)
}
