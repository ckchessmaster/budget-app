package categorization

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/ckchessmaster/budget-app/backend/pkg/fuzzy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

const (
	minSuggestionScore      = 0.25
	maxSuggestionsPerTx     = 4
	minFuzzySimilarity      = 0.78
	amountProximityRatio    = 0.15
	accountAffinityMinTxs   = 5
	accountAffinityMinShare = 0.75
	refundLookbackDays      = 90.0
)

type RecommendCategory struct {
	ID        uuid.UUID
	Name      string
	GroupName string
}

type RecommendPlannedIncome struct {
	ID   uuid.UUID
	Name string
}

type RecommendTargetTx struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	Amount        decimal.Decimal
	MerchantRaw   string
	MerchantClean *string
	Date          time.Time
}

type RecommendHistoricalTx struct {
	AccountID         uuid.UUID
	Amount            decimal.Decimal
	MerchantRaw       string
	MerchantClean     *string
	CategoryID        *uuid.UUID
	PlannedIncomeName *string
	Date              time.Time
}

type RecommendInput struct {
	Now            time.Time
	Targets        []RecommendTargetTx
	History        []RecommendHistoricalTx
	Categories     []RecommendCategory
	PlannedIncomes []RecommendPlannedIncome
}

type keywordGroup struct {
	merchantTerms []string
	categoryTerms []string
}

var keywordGroups = []keywordGroup{
	{
		merchantTerms: []string{"grocery", "groceries", "market", "trader joe", "whole foods", "kroger", "safeway", "aldi", "publix", "wegmans", "sprouts", "heb", "costco", "sam's club"},
		categoryTerms: []string{"grocer", "supermarket", "food"},
	},
	{
		merchantTerms: []string{"cafe", "coffee", "starbucks", "dunkin", "peet", "restaurant", "dining", "chipotle", "doordash", "ubereats", "grubhub", "pizza", "bakery", "bistro", "panera", "chick-fil-a", "mcdonald", "taco bell"},
		categoryTerms: []string{"dining", "restaurant", "coffee", "eating out", "takeout", "food"},
	},
	{
		merchantTerms: []string{"gas", "shell", "chevron", "exxon", "mobil", "bp", "sunoco", "uber", "lyft", "transit", "metro", "parking", "toll", "jiffy lube", "autozone", "tire"},
		categoryTerms: []string{"gas", "fuel", "transport", "auto", "car", "commut"},
	},
	{
		merchantTerms: []string{"airline", "delta", "united", "american airlines", "southwest", "jetblue", "hotel", "airbnb", "vrbo", "marriott", "hilton", "expedia"},
		categoryTerms: []string{"travel", "vacation", "flight", "hotel", "lodging"},
	},
	{
		merchantTerms: []string{"netflix", "spotify", "hulu", "apple", "subscription", "disney", "youtube", "patreon", "icloud", "prime", "openai", "adobe", "dropbox", "ny times"},
		categoryTerms: []string{"subscription", "stream", "entertainment", "media", "software"},
	},
	{
		merchantTerms: []string{"electric", "water", "utility", "utilities", "power", "internet", "comcast", "xfinity", "verizon", "at&t", "t-mobile", "spectrum", "pg&e", "coned"},
		categoryTerms: []string{"utilit", "internet", "phone", "electric", "water", "bill"},
	},
	{
		merchantTerms: []string{"pharmacy", "cvs", "walgreens", "rite aid", "clinic", "dental", "dentist", "medical", "doctor", "hospital", "optometry", "urgent care"},
		categoryTerms: []string{"health", "medical", "pharmacy", "dental", "vision"},
	},
	{
		merchantTerms: []string{"amazon", "target", "walmart", "best buy", "etsy", "home depot", "lowe's", "lowes", "ikea", "wayfair", "ace hardware"},
		categoryTerms: []string{"shopping", "home", "household", "furnish", "repair", "maintenance", "electronics", "supply", "supplies"},
	},
	{
		merchantTerms: []string{"chewy", "petco", "petsmart", "vet", "veterinary"},
		categoryTerms: []string{"pet", "vet", "animal"},
	},
	{
		merchantTerms: []string{"gym", "fitness", "planet fitness", "equinox", "peloton", "ymca", "orangetheory"},
		categoryTerms: []string{"fitness", "gym", "wellness", "health"},
	},
	{
		merchantTerms: []string{"geico", "progressive", "state farm", "allstate", "usaa", "insurance"},
		categoryTerms: []string{"insurance", "auto", "home"},
	},
	{
		merchantTerms: []string{"tithe", "church", "ministry", "charity", "donation", "red cross", "food pantry"},
		categoryTerms: []string{"giving", "charity", "tithe", "donat"},
	},
	{
		merchantTerms: []string{"tuition", "university", "coursera", "udemy", "school", "bookstore"},
		categoryTerms: []string{"education", "tuition", "school", "learning"},
	},
	{
		merchantTerms: []string{"payroll", "direct dep", "salary", "paycheck", "gusto", "adp", "workday"},
		categoryTerms: []string{"paycheck", "salary", "payroll", "income"},
	},
	{
		merchantTerms: []string{"interest", "dividend", "cashback", "cash back", "rebate"},
		categoryTerms: []string{"interest", "dividend", "income", "bonus"},
	},
}

func normalizedMerchantName(raw string, clean *string) string {
	if clean != nil {
		if c := strings.ToLower(strings.TrimSpace(*clean)); c != "" {
			return c
		}
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func merchantSimilarityScore(target, candidate string) float64 {
	if target == "" || candidate == "" {
		return 0
	}
	if target == candidate {
		return 1.0
	}
	if len(target) >= 4 && len(candidate) >= 4 && (strings.Contains(target, candidate) || strings.Contains(candidate, target)) {
		return 0.75
	}
	sim := fuzzy.Similarity(target, candidate)
	if sim >= minFuzzySimilarity {
		return 0.65 * sim
	}
	return 0
}

func isAmountSimilar(a, b decimal.Decimal) bool {
	absA := a.Abs().InexactFloat64()
	absB := b.Abs().InexactFloat64()
	maxV := math.Max(absA, absB)
	if maxV <= 0 {
		return false
	}
	return math.Abs(absA-absB)/maxV <= amountProximityRatio
}

type accountAffinity struct {
	categoryID uuid.UUID
}

func computeAccountAffinities(history []RecommendHistoricalTx) map[uuid.UUID]accountAffinity {
	byAcct := make(map[uuid.UUID]map[uuid.UUID]int)
	totals := make(map[uuid.UUID]int)
	for _, h := range history {
		if h.CategoryID == nil || *h.CategoryID == uuid.Nil {
			continue
		}
		if byAcct[h.AccountID] == nil {
			byAcct[h.AccountID] = make(map[uuid.UUID]int)
		}
		byAcct[h.AccountID][*h.CategoryID]++
		totals[h.AccountID]++
	}

	res := make(map[uuid.UUID]accountAffinity)
	for acctID, counts := range byAcct {
		total := totals[acctID]
		if total < accountAffinityMinTxs {
			continue
		}
		var topCat uuid.UUID
		topCount := 0
		for catID, c := range counts {
			if c > topCount {
				topCount = c
				topCat = catID
			}
		}
		if float64(topCount)/float64(total) >= accountAffinityMinShare {
			res[acctID] = accountAffinity{categoryID: topCat}
		}
	}
	return res
}

type scoredCandidate struct {
	suggestion      Suggestion
	histCount       int
	refundCount     int
	similarAmount   bool
	accountAffinity bool
	keywordMatch    bool
	nameMatch       bool
}

// ComputeSuggestions computes ranked, thresholded, sign-aware suggestions for every target transaction.
// It never pads results with unrelated fallback categories.
func ComputeSuggestions(input RecommendInput) map[uuid.UUID][]Suggestion {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	catByID := make(map[uuid.UUID]RecommendCategory, len(input.Categories))
	for _, c := range input.Categories {
		catByID[c.ID] = c
	}
	incomeByLowerName := make(map[string]RecommendPlannedIncome, len(input.PlannedIncomes))
	for _, inc := range input.PlannedIncomes {
		incomeByLowerName[strings.ToLower(strings.TrimSpace(inc.Name))] = inc
	}

	acctAffinities := computeAccountAffinities(input.History)
	out := make(map[uuid.UUID][]Suggestion, len(input.Targets))

	for _, tx := range input.Targets {
		targetMerchant := normalizedMerchantName(tx.MerchantRaw, tx.MerchantClean)
		isInflow := tx.Amount.Sign() > 0

		candidates := make(map[string]*scoredCandidate)
		ensureCategoryCandidate := func(cat RecommendCategory) *scoredCandidate {
			key := "cat:" + cat.ID.String()
			c := candidates[key]
			if c == nil {
				id := cat.ID
				c = &scoredCandidate{
					suggestion: Suggestion{
						TargetType: TargetTypeCategory,
						CategoryID: &id,
						Name:       cat.Name,
						GroupName:  cat.GroupName,
					},
				}
				candidates[key] = c
			}
			return c
		}
		ensureIncomeCandidate := func(inc RecommendPlannedIncome) *scoredCandidate {
			key := "inc:" + inc.ID.String()
			c := candidates[key]
			if c == nil {
				id := inc.ID
				c = &scoredCandidate{
					suggestion: Suggestion{
						TargetType:      TargetTypePlannedIncome,
						PlannedIncomeID: &id,
						Name:            inc.Name,
						GroupName:       "INCOME",
					},
				}
				candidates[key] = c
			}
			return c
		}

		// 1 & 2. Historical merchant matches (recency-weighted + amount proximity + refund matching)
		if targetMerchant != "" {
			for _, h := range input.History {
				histMerchant := normalizedMerchantName(h.MerchantRaw, h.MerchantClean)
				mWeight := merchantSimilarityScore(targetMerchant, histMerchant)
				if mWeight <= 0 {
					continue
				}

				ageDays := now.Sub(h.Date).Hours() / 24.0
				if ageDays < 0 {
					ageDays = 0
				}
				recencyDecay := math.Pow(0.5, ageDays/90.0)
				similarAmt := isAmountSimilar(tx.Amount, h.Amount)

				if isInflow {
					// Historical planned income match by normalized name
					if h.PlannedIncomeName != nil && *h.PlannedIncomeName != "" {
						if inc, ok := incomeByLowerName[strings.ToLower(strings.TrimSpace(*h.PlannedIncomeName))]; ok {
							sc := ensureIncomeCandidate(inc)
							delta := mWeight * recencyDecay
							if similarAmt {
								delta += 0.45 * recencyDecay
								sc.similarAmount = true
							}
							sc.suggestion.Score += delta
							sc.histCount++
						}
					}
					// Refund match: recent outflow at same merchant
					if h.CategoryID != nil && h.Amount.Sign() < 0 && ageDays <= refundLookbackDays {
						if cat, ok := catByID[*h.CategoryID]; ok {
							sc := ensureCategoryCandidate(cat)
							delta := 0.90 * mWeight * recencyDecay
							if similarAmt {
								delta += 0.45 * recencyDecay
								sc.similarAmount = true
							}
							sc.suggestion.Score += delta
							sc.refundCount++
						}
					}
				} else {
					// Expense: only recommend Categories
					if h.CategoryID != nil {
						if cat, ok := catByID[*h.CategoryID]; ok {
							sc := ensureCategoryCandidate(cat)
							signFactor := 1.0
							if h.Amount.Sign() > 0 {
								signFactor = 0.5
							}
							delta := mWeight * recencyDecay * signFactor
							if similarAmt && h.Amount.Sign() < 0 {
								delta += 0.45 * recencyDecay
								sc.similarAmount = true
							}
							sc.suggestion.Score += delta
							sc.histCount++
						}
					}
				}
			}
		}

		// 3. Account affinity (for outflows)
		if !isInflow {
			if aff, ok := acctAffinities[tx.AccountID]; ok {
				if cat, exists := catByID[aff.categoryID]; exists {
					sc := ensureCategoryCandidate(cat)
					sc.suggestion.Score += 0.35
					sc.accountAffinity = true
				}
			}
		}

		// 4. Expanded keyword + name token overlap
		if targetMerchant != "" {
			words := strings.FieldsFunc(targetMerchant, func(r rune) bool {
				return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
			})

			if isInflow {
				for _, inc := range input.PlannedIncomes {
					incLower := strings.ToLower(inc.Name)
					matchedName := false
					for _, w := range words {
						if len(w) >= 3 && strings.Contains(incLower, w) {
							matchedName = true
							break
						}
					}
					matchedKW := false
					if !matchedName {
						for _, kw := range keywordGroups {
							if !containsAny(targetMerchant, kw.merchantTerms) {
								continue
							}
							if containsAny(incLower, kw.categoryTerms) {
								matchedKW = true
								break
							}
						}
					}
					if matchedName || matchedKW {
						sc := ensureIncomeCandidate(inc)
						sc.suggestion.Score += 0.32
						if matchedName {
							sc.nameMatch = true
						} else {
							sc.keywordMatch = true
						}
					}
				}
			} else {
				for _, cat := range input.Categories {
					catText := strings.ToLower(cat.Name + " " + cat.GroupName)
					catNameLower := strings.ToLower(cat.Name)
					matchedName := false
					for _, w := range words {
						if len(w) >= 3 && (strings.Contains(catNameLower, w) || strings.Contains(catText, w)) {
							matchedName = true
							break
						}
					}
					if matchedName {
						sc := ensureCategoryCandidate(cat)
						sc.suggestion.Score += 0.32
						sc.nameMatch = true
						continue
					}

					for _, kw := range keywordGroups {
						if !containsAny(targetMerchant, kw.merchantTerms) {
							continue
						}
						if containsAny(catText, kw.categoryTerms) {
							sc := ensureCategoryCandidate(cat)
							sc.suggestion.Score += 0.30
							sc.keywordMatch = true
							break
						}
					}
				}
			}
		}

		// Format reasons and threshold
		var qualified []Suggestion
		for _, c := range candidates {
			if c.suggestion.Score < minSuggestionScore {
				continue
			}
			c.suggestion.Score = math.Round(c.suggestion.Score*1000) / 1000
			c.suggestion.Reason = formatSuggestionReason(c)
			qualified = append(qualified, c.suggestion)
		}

		sort.Slice(qualified, func(i, j int) bool {
			if qualified[i].Score != qualified[j].Score {
				return qualified[i].Score > qualified[j].Score
			}
			return qualified[i].Name < qualified[j].Name
		})

		if len(qualified) > maxSuggestionsPerTx {
			qualified = qualified[:maxSuggestionsPerTx]
		}
		if len(qualified) > 0 {
			out[tx.ID] = qualified
		}
	}

	return out
}

func containsAny(haystack string, terms []string) bool {
	for _, t := range terms {
		if strings.Contains(haystack, t) {
			return true
		}
	}
	return false
}

func formatSuggestionReason(c *scoredCandidate) string {
	switch {
	case c.refundCount > 0:
		if c.similarAmount {
			return "Refund match · similar amount"
		}
		return "Refund match"
	case c.histCount > 0:
		base := "Used before"
		if c.histCount > 1 {
			base = fmt.Sprintf("Used %d×", c.histCount)
		}
		if c.similarAmount {
			return base + " · similar amount"
		}
		return base
	case c.accountAffinity:
		return "Common for account"
	case c.nameMatch:
		return "Name match"
	case c.keywordMatch:
		return "Keyword"
	default:
		return "Suggested"
	}
}

// SuggestForMonth loads workspace categories, planned incomes, historical categorized items,
// and target transactions for a month (or single transaction ID) and returns ranked suggestions.
func SuggestForMonth(
	ctx context.Context,
	tx pgx.Tx,
	yearMonth time.Time,
	singleTxID *uuid.UUID,
) (map[uuid.UUID][]Suggestion, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}

	monthStart := time.Date(yearMonth.UTC().Year(), yearMonth.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	// 1. Active categories
	catRows, err := tx.Query(ctx, `
		SELECT id, name, group_name
		FROM categories
		WHERE workspace_id = $1 AND is_archived = FALSE
		ORDER BY group_name ASC, sort_order ASC, name ASC
	`, wsID)
	if err != nil {
		return nil, err
	}
	var categories []RecommendCategory
	for catRows.Next() {
		var c RecommendCategory
		if err := catRows.Scan(&c.ID, &c.Name, &c.GroupName); err != nil {
			catRows.Close()
			return nil, err
		}
		categories = append(categories, c)
	}
	catRows.Close()
	if err := catRows.Err(); err != nil {
		return nil, err
	}

	// 2. Planned incomes for the requested month
	incRows, err := tx.Query(ctx, `
		SELECT id, name
		FROM monthly_planned_incomes
		WHERE workspace_id = $1 AND year_month = $2
		ORDER BY created_at ASC
	`, wsID, monthStart.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	var incomes []RecommendPlannedIncome
	for incRows.Next() {
		var inc RecommendPlannedIncome
		if err := incRows.Scan(&inc.ID, &inc.Name); err != nil {
			incRows.Close()
			return nil, err
		}
		incomes = append(incomes, inc)
	}
	incRows.Close()
	if err := incRows.Err(); err != nil {
		return nil, err
	}

	// 3. Target transactions
	var targetRows pgx.Rows
	if singleTxID != nil && *singleTxID != uuid.Nil {
		targetRows, err = tx.Query(ctx, `
			SELECT t.id, t.account_id, t.amount, t.merchant_raw, t.merchant_clean, t.transaction_date
			FROM transactions t
			JOIN accounts a ON a.id = t.account_id
			WHERE t.workspace_id = $1
			  AND t.id = $2
			  AND a.is_on_budget = TRUE
			  AND t.parent_transaction_id IS NULL
			  AND t.is_transfer = FALSE
		`, wsID, *singleTxID)
	} else {
		targetRows, err = tx.Query(ctx, `
			SELECT t.id, t.account_id, t.amount, t.merchant_raw, t.merchant_clean, t.transaction_date
			FROM transactions t
			JOIN accounts a ON a.id = t.account_id
			WHERE t.workspace_id = $1
			  AND a.is_on_budget = TRUE
			  AND t.parent_transaction_id IS NULL
			  AND t.is_transfer = FALSE
			  AND t.transaction_date >= $2 AND t.transaction_date < $3
			ORDER BY t.transaction_date DESC
			LIMIT 250
		`, wsID, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"))
	}
	if err != nil {
		return nil, err
	}
	var targets []RecommendTargetTx
	for targetRows.Next() {
		var t RecommendTargetTx
		if err := targetRows.Scan(&t.ID, &t.AccountID, &t.Amount, &t.MerchantRaw, &t.MerchantClean, &t.Date); err != nil {
			targetRows.Close()
			return nil, err
		}
		targets = append(targets, t)
	}
	targetRows.Close()
	if err := targetRows.Err(); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return map[uuid.UUID][]Suggestion{}, nil
	}

	// 4. Historical categorized transactions (last 18 months, up to 2000 rows, including child splits)
	since := time.Now().UTC().AddDate(0, -18, 0).Format("2006-01-02")
	histRows, err := tx.Query(ctx, `
		SELECT t.account_id, t.amount, t.merchant_raw, t.merchant_clean, t.category_id, mpi.name, t.transaction_date
		FROM transactions t
		JOIN accounts a ON a.id = t.account_id
		LEFT JOIN monthly_planned_incomes mpi ON mpi.id = t.planned_income_id
		WHERE t.workspace_id = $1
		  AND a.is_on_budget = TRUE
		  AND t.is_transfer = FALSE
		  AND (t.category_id IS NOT NULL OR t.planned_income_id IS NOT NULL)
		  AND t.transaction_date >= $2
		ORDER BY t.transaction_date DESC, t.created_at DESC
		LIMIT 2000
	`, wsID, since)
	if err != nil {
		return nil, err
	}
	var history []RecommendHistoricalTx
	for histRows.Next() {
		var h RecommendHistoricalTx
		if err := histRows.Scan(&h.AccountID, &h.Amount, &h.MerchantRaw, &h.MerchantClean, &h.CategoryID, &h.PlannedIncomeName, &h.Date); err != nil {
			histRows.Close()
			return nil, err
		}
		history = append(history, h)
	}
	histRows.Close()
	if err := histRows.Err(); err != nil {
		return nil, err
	}

	return ComputeSuggestions(RecommendInput{
		Now:            time.Now().UTC(),
		Targets:        targets,
		History:        history,
		Categories:     categories,
		PlannedIncomes: incomes,
	}), nil
}
