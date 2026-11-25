package categorization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func normalizeRuleFields(
	matchType, pattern, direction string,
	minAmt, maxAmt *decimal.Decimal,
	catID *uuid.UUID,
	incomeName, renameMerchant *string,
) (Rule, error) {
	pat := strings.TrimSpace(pattern)
	if pat == "" {
		return Rule{}, fmt.Errorf("%w: merchant_pattern is required", ErrInvalidRule)
	}

	mt := strings.ToUpper(strings.TrimSpace(matchType))
	if mt == "" {
		mt = MatchTypeContains
	}
	if mt != MatchTypeContains && mt != MatchTypeExact {
		return Rule{}, fmt.Errorf("%w: invalid match_type", ErrInvalidRule)
	}

	dir := strings.ToUpper(strings.TrimSpace(direction))
	if dir == "" {
		dir = DirectionAny
	}
	if dir != DirectionAny && dir != DirectionExpense && dir != DirectionInflow {
		return Rule{}, fmt.Errorf("%w: invalid direction", ErrInvalidRule)
	}

	var normMin, normMax *decimal.Decimal
	if minAmt != nil {
		v := minAmt.Abs().Round(2)
		normMin = &v
	}
	if maxAmt != nil {
		v := maxAmt.Abs().Round(2)
		normMax = &v
	}
	if normMin != nil && normMax != nil && normMin.GreaterThan(*normMax) {
		return Rule{}, fmt.Errorf("%w: min_amount cannot exceed max_amount", ErrInvalidRule)
	}

	var cleanCatID *uuid.UUID
	if catID != nil && *catID != uuid.Nil {
		id := *catID
		cleanCatID = &id
	}

	var cleanIncome *string
	if incomeName != nil {
		trimmed := strings.TrimSpace(*incomeName)
		if trimmed != "" {
			cleanIncome = &trimmed
		}
	}

	hasCat := cleanCatID != nil
	hasInc := cleanIncome != nil
	if hasCat == hasInc {
		return Rule{}, fmt.Errorf("%w: exactly one of category_id or planned_income_name is required", ErrInvalidRule)
	}

	var cleanRename *string
	if renameMerchant != nil {
		trimmed := strings.TrimSpace(*renameMerchant)
		if trimmed != "" {
			cleanRename = &trimmed
		}
	}

	return Rule{
		MatchType:         mt,
		MerchantPattern:   pat,
		Direction:         dir,
		MinAmount:         normMin,
		MaxAmount:         normMax,
		CategoryID:        cleanCatID,
		PlannedIncomeName: cleanIncome,
		RenameMerchant:    cleanRename,
	}, nil
}

func ListRules(ctx context.Context, tx pgx.Tx) ([]Rule, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, priority, match_type, merchant_pattern, account_id,
		       direction, min_amount, max_amount, category_id, planned_income_name,
		       rename_merchant, require_review, is_active, created_at, updated_at
		FROM categorization_rules
		WHERE workspace_id = $1
		ORDER BY priority ASC, created_at ASC
	`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(
			&r.ID, &r.WorkspaceID, &r.Priority, &r.MatchType, &r.MerchantPattern, &r.AccountID,
			&r.Direction, &r.MinAmount, &r.MaxAmount, &r.CategoryID, &r.PlannedIncomeName,
			&r.RenameMerchant, &r.RequireReview, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func GetRule(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Rule, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	var r Rule
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, priority, match_type, merchant_pattern, account_id,
		       direction, min_amount, max_amount, category_id, planned_income_name,
		       rename_merchant, require_review, is_active, created_at, updated_at
		FROM categorization_rules
		WHERE id = $1 AND workspace_id = $2
	`, id, wsID).Scan(
		&r.ID, &r.WorkspaceID, &r.Priority, &r.MatchType, &r.MerchantPattern, &r.AccountID,
		&r.Direction, &r.MinAmount, &r.MaxAmount, &r.CategoryID, &r.PlannedIncomeName,
		&r.RenameMerchant, &r.RequireReview, &r.IsActive, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRuleNotFound
		}
		return nil, err
	}
	return &r, nil
}

func CreateRule(ctx context.Context, tx pgx.Tx, req CreateRuleRequest) (*Rule, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	norm, err := normalizeRuleFields(
		req.MatchType, req.MerchantPattern, req.Direction,
		req.MinAmount, req.MaxAmount,
		req.CategoryID, req.PlannedIncomeName, req.RenameMerchant,
	)
	if err != nil {
		return nil, err
	}

	var acctID *uuid.UUID
	if req.AccountID != nil && *req.AccountID != uuid.Nil {
		id := *req.AccountID
		acctID = &id
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	var nextPriority int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(priority), -1) + 1
		FROM categorization_rules
		WHERE workspace_id = $1
	`, wsID).Scan(&nextPriority); err != nil {
		return nil, err
	}

	r := Rule{
		ID:                uuid.New(),
		WorkspaceID:       wsID,
		Priority:          nextPriority,
		MatchType:         norm.MatchType,
		MerchantPattern:   norm.MerchantPattern,
		AccountID:         acctID,
		Direction:         norm.Direction,
		MinAmount:         norm.MinAmount,
		MaxAmount:         norm.MaxAmount,
		CategoryID:        norm.CategoryID,
		PlannedIncomeName: norm.PlannedIncomeName,
		RenameMerchant:    norm.RenameMerchant,
		RequireReview:     req.RequireReview,
		IsActive:          isActive,
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO categorization_rules (
			id, workspace_id, priority, match_type, merchant_pattern, account_id,
			direction, min_amount, max_amount, category_id, planned_income_name,
			rename_merchant, require_review, is_active
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING created_at, updated_at
	`,
		r.ID, r.WorkspaceID, r.Priority, r.MatchType, r.MerchantPattern, r.AccountID,
		r.Direction, r.MinAmount, r.MaxAmount, r.CategoryID, r.PlannedIncomeName,
		r.RenameMerchant, r.RequireReview, r.IsActive,
	).Scan(&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func UpdateRule(ctx context.Context, tx pgx.Tx, id uuid.UUID, req UpdateRuleRequest) (*Rule, error) {
	existing, err := GetRule(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	norm, err := normalizeRuleFields(
		req.MatchType, req.MerchantPattern, req.Direction,
		req.MinAmount, req.MaxAmount,
		req.CategoryID, req.PlannedIncomeName, req.RenameMerchant,
	)
	if err != nil {
		return nil, err
	}

	var acctID *uuid.UUID
	if req.AccountID != nil && *req.AccountID != uuid.Nil {
		v := *req.AccountID
		acctID = &v
	}

	isActive := existing.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	existing.MatchType = norm.MatchType
	existing.MerchantPattern = norm.MerchantPattern
	existing.AccountID = acctID
	existing.Direction = norm.Direction
	existing.MinAmount = norm.MinAmount
	existing.MaxAmount = norm.MaxAmount
	existing.CategoryID = norm.CategoryID
	existing.PlannedIncomeName = norm.PlannedIncomeName
	existing.RenameMerchant = norm.RenameMerchant
	existing.RequireReview = req.RequireReview
	existing.IsActive = isActive

	err = tx.QueryRow(ctx, `
		UPDATE categorization_rules
		SET match_type = $3,
		    merchant_pattern = $4,
		    account_id = $5,
		    direction = $6,
		    min_amount = $7,
		    max_amount = $8,
		    category_id = $9,
		    planned_income_name = $10,
		    rename_merchant = $11,
		    require_review = $12,
		    is_active = $13,
		    updated_at = NOW()
		WHERE id = $1 AND workspace_id = $2
		RETURNING updated_at
	`,
		existing.ID, existing.WorkspaceID, existing.MatchType, existing.MerchantPattern, existing.AccountID,
		existing.Direction, existing.MinAmount, existing.MaxAmount, existing.CategoryID, existing.PlannedIncomeName,
		existing.RenameMerchant, existing.RequireReview, existing.IsActive,
	).Scan(&existing.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return existing, nil
}

func DeleteRule(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM categorization_rules WHERE id = $1 AND workspace_id = $2`, id, wsID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	return nil
}

func ReorderRules(ctx context.Context, tx pgx.Tx, ruleIDs []uuid.UUID) ([]Rule, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	for idx, id := range ruleIDs {
		if _, err := tx.Exec(ctx, `
			UPDATE categorization_rules
			SET priority = $3, updated_at = NOW()
			WHERE id = $1 AND workspace_id = $2
		`, id, wsID, idx); err != nil {
			return nil, err
		}
	}
	return ListRules(ctx, tx)
}

// ApplyRulesToWorkspace evaluates active workspace rules in priority order against
// uncategorized, non-transfer, non-split parent transactions on budget accounts.
func ApplyRulesToWorkspace(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) (int, error) {
	rules, err := ListRules(ctx, tx)
	if err != nil {
		return 0, err
	}
	return applyRulesToWorkspaceWithSet(ctx, tx, workspaceID, rules)
}

// ApplyRuleByID runs a single rule retroactively against uncategorized eligible transactions.
func ApplyRuleByID(ctx context.Context, tx pgx.Tx, ruleID uuid.UUID) (int, error) {
	rule, err := GetRule(ctx, tx, ruleID)
	if err != nil {
		return 0, err
	}
	if !rule.IsActive {
		return 0, nil
	}
	return applyRulesToWorkspaceWithSet(ctx, tx, rule.WorkspaceID, []Rule{*rule})
}

func applyRulesToWorkspaceWithSet(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, rules []Rule) (int, error) {
	activeRules := make([]Rule, 0, len(rules))
	needsIncomeLookup := false
	for _, r := range rules {
		if r.IsActive {
			activeRules = append(activeRules, r)
			if r.PlannedIncomeName != nil && *r.PlannedIncomeName != "" {
				needsIncomeLookup = true
			}
		}
	}
	if len(activeRules) == 0 {
		return 0, nil
	}

	type incomeKey struct {
		monthKey  string
		nameLower string
	}
	incomeMap := make(map[incomeKey]uuid.UUID)
	if needsIncomeLookup {
		iRows, err := tx.Query(ctx, `
			SELECT id, year_month, LOWER(TRIM(name))
			FROM monthly_planned_incomes
			WHERE workspace_id = $1
		`, workspaceID)
		if err != nil {
			return 0, err
		}
		for iRows.Next() {
			var id uuid.UUID
			var ym time.Time
			var nameLower string
			if err := iRows.Scan(&id, &ym, &nameLower); err != nil {
				iRows.Close()
				return 0, err
			}
			incomeMap[incomeKey{monthKey: ym.UTC().Format("2006-01"), nameLower: nameLower}] = id
		}
		iRows.Close()
		if err := iRows.Err(); err != nil {
			return 0, err
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT t.id, t.account_id, t.amount, t.merchant_raw, t.merchant_clean, t.transaction_date
		FROM transactions t
		JOIN accounts a ON a.id = t.account_id
		WHERE t.workspace_id = $1
		  AND a.is_on_budget = TRUE
		  AND t.parent_transaction_id IS NULL
		  AND t.is_transfer = FALSE
		  AND t.category_id IS NULL
		  AND t.planned_income_id IS NULL
		  AND NOT EXISTS (
			  SELECT 1 FROM transactions c
			  WHERE c.parent_transaction_id = t.id AND c.workspace_id = $1
		  )
	`, workspaceID)
	if err != nil {
		return 0, err
	}

	type candidate struct {
		id            uuid.UUID
		accountID     uuid.UUID
		amount        decimal.Decimal
		merchantRaw   string
		merchantClean *string
		txDate        time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.accountID, &c.amount, &c.merchantRaw, &c.merchantClean, &c.txDate); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	updated := 0
	for _, c := range candidates {
		mtx := MatchableTx{
			MerchantRaw:   c.merchantRaw,
			MerchantClean: c.merchantClean,
			AccountID:     c.accountID,
			Amount:        c.amount,
		}
		var matched *Rule
		var resolvedIncomeID *uuid.UUID
		for i := range activeRules {
			r := &activeRules[i]
			if !RuleMatches(*r, mtx) {
				continue
			}
			if r.PlannedIncomeName != nil && *r.PlannedIncomeName != "" {
				incID, ok := incomeMap[incomeKey{
					monthKey:  c.txDate.UTC().Format("2006-01"),
					nameLower: strings.ToLower(strings.TrimSpace(*r.PlannedIncomeName)),
				}]
				if !ok {
					// Planned income does not exist in this transaction's month; skip rule.
					continue
				}
				resolvedIncomeID = &incID
			}
			matched = r
			break
		}
		if matched == nil {
			continue
		}

		nextClean := c.merchantClean
		if matched.RenameMerchant != nil && strings.TrimSpace(*matched.RenameMerchant) != "" {
			v := strings.TrimSpace(*matched.RenameMerchant)
			nextClean = &v
		}

		var catID, incID *uuid.UUID
		if matched.CategoryID != nil {
			catID = matched.CategoryID
		} else {
			incID = resolvedIncomeID
		}

		if _, err := tx.Exec(ctx, `
			UPDATE transactions
			SET category_id = $3,
			    planned_income_id = $4,
			    merchant_clean = $5,
			    categorized_by = 'RULE',
			    applied_rule_id = $6,
			    needs_review = $7
			WHERE id = $1 AND workspace_id = $2
		`, c.id, workspaceID, catID, incID, nextClean, matched.ID, matched.RequireReview); err != nil {
			return 0, err
		}
		updated++
	}

	return updated, nil
}

// ApplySimilar assigns category_id or planned_income_id to matching transactions for the same merchant.
func ApplySimilar(ctx context.Context, tx pgx.Tx, req ApplySimilarRequest) (int, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return 0, err
	}

	pat := strings.TrimSpace(req.MerchantPattern)
	if pat == "" {
		return 0, fmt.Errorf("%w: merchant_pattern is required", ErrInvalidApplyRequest)
	}
	hasCat := req.CategoryID != nil && *req.CategoryID != uuid.Nil
	hasInc := req.PlannedIncomeID != nil && *req.PlannedIncomeID != uuid.Nil
	if hasCat == hasInc {
		return 0, fmt.Errorf("%w: exactly one of category_id or planned_income_id is required", ErrInvalidApplyRequest)
	}

	mt := strings.ToUpper(strings.TrimSpace(req.MatchType))
	if mt == "" {
		mt = MatchTypeContains
	}
	if mt != MatchTypeContains && mt != MatchTypeExact {
		return 0, fmt.Errorf("%w: invalid match_type", ErrInvalidApplyRequest)
	}

	dir := strings.ToUpper(strings.TrimSpace(req.Direction))
	if dir == "" {
		dir = DirectionAny
	}
	if dir != DirectionAny && dir != DirectionExpense && dir != DirectionInflow {
		return 0, fmt.Errorf("%w: invalid direction", ErrInvalidApplyRequest)
	}

	onlyUncat := true
	if req.OnlyUncategorized != nil {
		onlyUncat = *req.OnlyUncategorized
	}

	rows, err := tx.Query(ctx, `
		SELECT t.id, t.amount, t.merchant_raw, t.merchant_clean, t.category_id, t.planned_income_id
		FROM transactions t
		JOIN accounts a ON a.id = t.account_id
		WHERE t.workspace_id = $1
		  AND a.is_on_budget = TRUE
		  AND t.parent_transaction_id IS NULL
		  AND t.is_transfer = FALSE
		  AND NOT EXISTS (
			  SELECT 1 FROM transactions c
			  WHERE c.parent_transaction_id = t.id AND c.workspace_id = $1
		  )
	`, wsID)
	if err != nil {
		return 0, err
	}

	type rowItem struct {
		id            uuid.UUID
		amount        decimal.Decimal
		merchantRaw   string
		merchantClean *string
		categoryID    *uuid.UUID
		incomeID      *uuid.UUID
	}
	var candidates []rowItem
	for rows.Next() {
		var r rowItem
		if err := rows.Scan(&r.id, &r.amount, &r.merchantRaw, &r.merchantClean, &r.categoryID, &r.incomeID); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	updated := 0
	for _, c := range candidates {
		if onlyUncat && (c.categoryID != nil || c.incomeID != nil) {
			continue
		}
		if !DirectionMatches(dir, c.amount) {
			continue
		}
		if !PatternMatches(mt, pat, c.merchantRaw, c.merchantClean) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE transactions
			SET category_id = $3,
			    planned_income_id = $4,
			    categorized_by = 'MANUAL',
			    applied_rule_id = NULL,
			    needs_review = FALSE
			WHERE id = $1 AND workspace_id = $2
		`, c.id, wsID, req.CategoryID, req.PlannedIncomeID); err != nil {
			return 0, err
		}
		updated++
	}
	return updated, nil
}
