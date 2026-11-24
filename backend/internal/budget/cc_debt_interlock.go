package budget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// CCAccountSpend captures credit card spend attributed to a single credit card account.
type CCAccountSpend struct {
	AccountID uuid.UUID       `json:"account_id"`
	Amount    decimal.Decimal `json:"amount"`
}

// CategoryDeficitInput describes a category's end-of-month remaining balance and credit card spend breakdown.
type CategoryDeficitInput struct {
	CategoryID    uuid.UUID
	CategoryName  string
	Remaining     decimal.Decimal
	CCSpendByCard []CCAccountSpend
}

// CategoryCCDeficitDetail summarizes how a single overspent category splits between cash and credit card deficits.
type CategoryCCDeficitDetail struct {
	CategoryID        uuid.UUID        `json:"category_id"`
	CategoryName      string           `json:"category_name"`
	TotalDeficit      decimal.Decimal  `json:"total_deficit"`
	CCDeficit         decimal.Decimal  `json:"cc_deficit"`
	CashDeficit       decimal.Decimal  `json:"cash_deficit"`
	PerAccountDeficit []CCAccountSpend `json:"per_account_deficit"`
}

// CCAccountInterlock represents the uncovered credit card overspend attributed to a single card account.
type CCAccountInterlock struct {
	AccountID     *uuid.UUID      `json:"account_id"`
	DeficitAmount decimal.Decimal `json:"deficit_amount"`
}

// CCDebtInterlockPlan captures the month-end uncovered debt calculation for a workspace.
type CCDebtInterlockPlan struct {
	SourceMonth          time.Time                 `json:"source_month"`
	TargetMonth          time.Time                 `json:"target_month"`
	TotalCCDeficit       decimal.Decimal           `json:"total_cc_deficit"`
	TotalCashDeficit     decimal.Decimal           `json:"total_cash_deficit"`
	ReadyToAssignDelta   decimal.Decimal           `json:"ready_to_assign_delta"`
	RequiresNotification bool                      `json:"requires_notification"`
	AccountInterlocks    []CCAccountInterlock      `json:"account_interlocks"`
	CategoryDetails      []CategoryCCDeficitDetail `json:"category_details"`
}

func sortAccountKeys(spends map[uuid.UUID]decimal.Decimal) []uuid.UUID {
	keys := make([]uuid.UUID, 0, len(spends))
	for id := range spends {
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == uuid.Nil {
			return false
		}
		if keys[j] == uuid.Nil {
			return true
		}
		return keys[i].String() < keys[j].String()
	})
	return keys
}

// allocateCCDeficitAcrossCards distributes ccDeficit across cards proportionally using
// Hamilton's largest-remainder method in integer cents so allocations sum exactly to ccDeficit.
func allocateCCDeficitAcrossCards(
	ccDeficit decimal.Decimal,
	totalCCSpend decimal.Decimal,
	cardSpends map[uuid.UUID]decimal.Decimal,
) []CCAccountSpend {
	ccDeficit = roundMoney(ccDeficit)
	totalCCSpend = roundMoney(totalCCSpend)
	if !ccDeficit.GreaterThan(decimal.Zero) || len(cardSpends) == 0 {
		return []CCAccountSpend{}
	}

	keys := sortAccountKeys(cardSpends)
	if !ccDeficit.LessThan(totalCCSpend) {
		out := make([]CCAccountSpend, 0, len(keys))
		for _, id := range keys {
			amt := roundMoney(cardSpends[id])
			if amt.GreaterThan(decimal.Zero) {
				out = append(out, CCAccountSpend{AccountID: id, Amount: amt})
			}
		}
		return out
	}

	if len(keys) == 1 {
		return []CCAccountSpend{{AccountID: keys[0], Amount: ccDeficit}}
	}

	deficitCents := ccDeficit.Shift(2).BigInt()
	totalCents := totalCCSpend.Shift(2).BigInt()

	type cardShare struct {
		idx       int
		id        uuid.UUID
		capCents  *big.Int
		alloc     *big.Int
		remainder *big.Int
	}

	shares := make([]cardShare, len(keys))
	allocatedSum := big.NewInt(0)
	for i, id := range keys {
		capCents := roundMoney(cardSpends[id]).Shift(2).BigInt()
		num := new(big.Int).Mul(capCents, deficitCents)
		quot, rem := new(big.Int).QuoRem(num, totalCents, new(big.Int))
		if quot.Cmp(capCents) > 0 {
			quot.Set(capCents)
		}
		allocatedSum.Add(allocatedSum, quot)
		shares[i] = cardShare{
			idx:       i,
			id:        id,
			capCents:  capCents,
			alloc:     quot,
			remainder: rem,
		}
	}

	leftover := new(big.Int).Sub(deficitCents, allocatedSum)
	if leftover.Sign() > 0 {
		order := make([]int, len(shares))
		for i := range shares {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool {
			sa, sb := shares[order[a]], shares[order[b]]
			if cmp := sa.remainder.Cmp(sb.remainder); cmp != 0 {
				return cmp > 0
			}
			return sa.idx < sb.idx
		})

		one := big.NewInt(1)
		for leftover.Sign() > 0 {
			progressed := false
			for _, idx := range order {
				if leftover.Sign() == 0 {
					break
				}
				if shares[idx].alloc.Cmp(shares[idx].capCents) < 0 {
					shares[idx].alloc.Add(shares[idx].alloc, one)
					leftover.Sub(leftover, one)
					progressed = true
				}
			}
			if !progressed {
				break
			}
		}
	}

	out := make([]CCAccountSpend, 0, len(shares))
	for _, s := range shares {
		if s.alloc.Sign() > 0 {
			out = append(out, CCAccountSpend{
				AccountID: s.id,
				Amount:    roundMoney(decimal.NewFromBigInt(s.alloc, -2)),
			})
		}
	}
	return out
}

// EvaluateCCDebtInterlock determines uncovered credit card vs. cash overspending across categories
// at a month boundary and aggregates per-card interlock deficits targeting M+1.
func EvaluateCCDebtInterlock(sourceMonth time.Time, inputs []CategoryDeficitInput) CCDebtInterlockPlan {
	zero := decimal.Zero.Round(2)
	src := NormalizeYearMonth(sourceMonth)
	tgt := src.AddDate(0, 1, 0)

	plan := CCDebtInterlockPlan{
		SourceMonth:          src,
		TargetMonth:          tgt,
		TotalCCDeficit:       zero,
		TotalCashDeficit:     zero,
		ReadyToAssignDelta:   zero,
		RequiresNotification: false,
		AccountInterlocks:    make([]CCAccountInterlock, 0),
		CategoryDetails:      make([]CategoryCCDeficitDetail, 0),
	}

	workspaceCardTotals := make(map[uuid.UUID]decimal.Decimal)

	for _, in := range inputs {
		rem := roundMoney(in.Remaining)
		if !rem.LessThan(decimal.Zero) {
			continue
		}

		deficit := roundMoney(rem.Abs())
		cardSpends := make(map[uuid.UUID]decimal.Decimal)
		totalCCSpend := zero
		for _, s := range in.CCSpendByCard {
			amt := roundMoney(s.Amount)
			if !amt.GreaterThan(decimal.Zero) {
				continue
			}
			cardSpends[s.AccountID] = roundMoney(cardSpends[s.AccountID].Add(amt))
			totalCCSpend = roundMoney(totalCCSpend.Add(amt))
		}

		// Cash spending consumes budgeted dollars first; remaining overspend up to totalCCSpend is uncovered CC debt.
		ccDeficit := deficit
		if totalCCSpend.LessThan(ccDeficit) {
			ccDeficit = totalCCSpend
		}
		cashDeficit := roundMoney(deficit.Sub(ccDeficit))

		perAccount := allocateCCDeficitAcrossCards(ccDeficit, totalCCSpend, cardSpends)
		for _, alloc := range perAccount {
			workspaceCardTotals[alloc.AccountID] = roundMoney(workspaceCardTotals[alloc.AccountID].Add(alloc.Amount))
		}

		plan.TotalCCDeficit = roundMoney(plan.TotalCCDeficit.Add(ccDeficit))
		plan.TotalCashDeficit = roundMoney(plan.TotalCashDeficit.Add(cashDeficit))
		plan.CategoryDetails = append(plan.CategoryDetails, CategoryCCDeficitDetail{
			CategoryID:        in.CategoryID,
			CategoryName:      in.CategoryName,
			TotalDeficit:      deficit,
			CCDeficit:         ccDeficit,
			CashDeficit:       cashDeficit,
			PerAccountDeficit: perAccount,
		})
	}

	for _, id := range sortAccountKeys(workspaceCardTotals) {
		amt := roundMoney(workspaceCardTotals[id])
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}
		var acctPtr *uuid.UUID
		if id != uuid.Nil {
			cp := id
			acctPtr = &cp
		}
		plan.AccountInterlocks = append(plan.AccountInterlocks, CCAccountInterlock{
			AccountID:     acctPtr,
			DeficitAmount: amt,
		})
	}

	plan.ReadyToAssignDelta = roundMoney(plan.TotalCCDeficit.Add(plan.TotalCashDeficit).Neg())
	plan.RequiresNotification = plan.TotalCCDeficit.GreaterThan(decimal.Zero)
	return plan
}

// CCInterlockResolutionAction represents the user's choice for resolving an uncovered CC deficit.
type CCInterlockResolutionAction string

const (
	CCInterlockActionKeepDeduction CCInterlockResolutionAction = "KEEP_DEDUCTION"
	CCInterlockActionConvertToDebt CCInterlockResolutionAction = "CONVERT_TO_DEBT"
)

// CCDebtInterlockView enriches a persisted interlock with its account display name.
type CCDebtInterlockView struct {
	CreditCardDebtInterlock
	AccountName string `json:"account_name,omitempty"`
}

// ResolveCCDebtInterlockRequest validates an interlock resolution API payload.
type ResolveCCDebtInterlockRequest struct {
	Action CCInterlockResolutionAction `json:"action"`
}

func (r *ResolveCCDebtInterlockRequest) Validate() error {
	switch r.Action {
	case CCInterlockActionKeepDeduction, CCInterlockActionConvertToDebt:
		return nil
	default:
		return ErrInvalidInterlockAction
	}
}

// ResolveCCDebtInterlockResponse summarizes the outcome of resolving a pending CC debt interlock.
type ResolveCCDebtInterlockResponse struct {
	Interlock                  CreditCardDebtInterlock `json:"interlock"`
	ReadyToAssignRestored      decimal.Decimal         `json:"ready_to_assign_restored"`
	UpdatedInitialDebtBaseline *decimal.Decimal        `json:"updated_initial_debt_baseline,omitempty"`
}

func validateInterlockStatus(status CCInterlockStatus) error {
	switch status {
	case CCInterlockPending, CCInterlockKeepDeduction, CCInterlockConvertToDebt:
		return nil
	default:
		return ErrInvalidInterlockStatus
	}
}

// CCDebtInterlockService manages querying, persisting, and resolving month-end CC debt interlocks.
type CCDebtInterlockService interface {
	LoadCategoryCCSpendByMonth(ctx context.Context, tx pgx.Tx, sourceMonth time.Time) (map[uuid.UUID][]CCAccountSpend, error)
	PersistInterlockPlan(ctx context.Context, tx pgx.Tx, plan CCDebtInterlockPlan) ([]CreditCardDebtInterlock, error)
	ListInterlocks(ctx context.Context, tx pgx.Tx, targetMonth *time.Time, status *CCInterlockStatus) ([]CCDebtInterlockView, error)
	ResolveInterlock(ctx context.Context, tx pgx.Tx, interlockID uuid.UUID, action CCInterlockResolutionAction) (*ResolveCCDebtInterlockResponse, error)
}

type pgCCDebtInterlockService struct{}

// NewCCDebtInterlockService returns a PostgreSQL-backed CCDebtInterlockService.
func NewCCDebtInterlockService() CCDebtInterlockService {
	return &pgCCDebtInterlockService{}
}

func (s *pgCCDebtInterlockService) LoadCategoryCCSpendByMonth(
	ctx context.Context,
	tx pgx.Tx,
	sourceMonth time.Time,
) (map[uuid.UUID][]CCAccountSpend, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}

	ym := NormalizeYearMonth(sourceMonth)
	nextYM := ym.AddDate(0, 1, 0)

	rows, err := tx.Query(ctx, `
		SELECT t.category_id, t.account_id, COALESCE(SUM(-t.amount), 0) AS spent
		FROM transactions t
		INNER JOIN accounts a ON a.id = t.account_id
		WHERE t.workspace_id = $3
		  AND a.workspace_id = $3
		  AND t.category_id IS NOT NULL
		  AND t.is_transfer = FALSE
		  AND t.amount < 0
		  AND a.is_on_budget = TRUE
		  AND a.type = 'credit_card'
		  AND t.transaction_date >= $1::date
		  AND t.transaction_date < $2::date
		GROUP BY t.category_id, t.account_id
		ORDER BY t.category_id ASC, t.account_id ASC
	`, ym, nextYM, wsID)
	if err != nil {
		return nil, fmt.Errorf("failed to load category credit card spend: %w", err)
	}
	defer rows.Close()

	byCategory := make(map[uuid.UUID][]CCAccountSpend)
	for rows.Next() {
		var catID, acctID uuid.UUID
		var spent decimal.Decimal
		if err := rows.Scan(&catID, &acctID, &spent); err != nil {
			return nil, fmt.Errorf("failed to scan category credit card spend: %w", err)
		}
		amt := roundMoney(spent)
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}
		byCategory[catID] = append(byCategory[catID], CCAccountSpend{
			AccountID: acctID,
			Amount:    amt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating category credit card spend: %w", err)
	}
	return byCategory, nil
}

func (s *pgCCDebtInterlockService) PersistInterlockPlan(
	ctx context.Context,
	tx pgx.Tx,
	plan CCDebtInterlockPlan,
) ([]CreditCardDebtInterlock, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}

	src := NormalizeYearMonth(plan.SourceMonth)
	tgt := NormalizeYearMonth(plan.TargetMonth)
	if plan.TargetMonth.IsZero() {
		tgt = src.AddDate(0, 1, 0)
	}

	persisted := make([]CreditCardDebtInterlock, 0, len(plan.AccountInterlocks))
	for _, item := range plan.AccountInterlocks {
		amt := roundMoney(item.DeficitAmount)
		if !amt.GreaterThan(decimal.Zero) {
			continue
		}

		var acctID *uuid.UUID
		if item.AccountID != nil && *item.AccountID != uuid.Nil {
			cp := *item.AccountID
			acctID = &cp
		}

		var il CreditCardDebtInterlock
		var statusStr string
		err := tx.QueryRow(ctx, `
			INSERT INTO credit_card_debt_interlocks
				(workspace_id, account_id, source_month, target_month, deficit_amount, status)
			VALUES ($1, $2, $3::date, $4::date, $5, 'PENDING')
			ON CONFLICT (workspace_id, account_id, source_month) DO NOTHING
			RETURNING id, workspace_id, account_id, source_month, target_month, deficit_amount, status, resolved_at, created_at
		`, wsID, acctID, src, tgt, amt).Scan(
			&il.ID, &il.WorkspaceID, &il.AccountID, &il.SourceMonth, &il.TargetMonth,
			&il.DeficitAmount, &statusStr, &il.ResolvedAt, &il.CreatedAt,
		)
		if err == nil {
			il.Status = CCInterlockStatus(statusStr)
			il.DeficitAmount = roundMoney(il.DeficitAmount)
			reason := fmt.Sprintf("Uncovered credit card overspend from %s", src.Format("2006-01"))
			if _, adjErr := tx.Exec(ctx, `
				INSERT INTO monthly_rollover_adjustments
					(workspace_id, source_month, target_month, category_id, interlock_id, adjustment_type, amount, is_active, reason)
				VALUES ($1, $2::date, $3::date, NULL, $4, 'CC_DEFICIT_DEDUCTION', $5, TRUE, $6)
			`, wsID, src, tgt, il.ID, roundMoney(amt.Neg()), reason); adjErr != nil {
				return nil, fmt.Errorf("failed to insert cc deficit rollover adjustment: %w", adjErr)
			}
			persisted = append(persisted, il)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to insert credit card debt interlock: %w", err)
		}

		err = tx.QueryRow(ctx, `
			SELECT id, workspace_id, account_id, source_month, target_month, deficit_amount, status, resolved_at, created_at
			FROM credit_card_debt_interlocks
			WHERE workspace_id = $1
			  AND account_id IS NOT DISTINCT FROM $2
			  AND source_month = $3::date
		`, wsID, acctID, src).Scan(
			&il.ID, &il.WorkspaceID, &il.AccountID, &il.SourceMonth, &il.TargetMonth,
			&il.DeficitAmount, &statusStr, &il.ResolvedAt, &il.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch existing credit card debt interlock: %w", err)
		}
		il.Status = CCInterlockStatus(statusStr)
		il.DeficitAmount = roundMoney(il.DeficitAmount)
		persisted = append(persisted, il)
	}

	return persisted, nil
}

func (s *pgCCDebtInterlockService) ListInterlocks(
	ctx context.Context,
	tx pgx.Tx,
	targetMonth *time.Time,
	status *CCInterlockStatus,
) ([]CCDebtInterlockView, error) {
	if status != nil {
		if err := validateInterlockStatus(*status); err != nil {
			return nil, err
		}
	}

	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}

	args := []any{wsID}
	query := `
		SELECT i.id, i.workspace_id, i.account_id, COALESCE(a.name, '') AS account_name,
		       i.source_month, i.target_month, i.deficit_amount, i.status, i.resolved_at, i.created_at
		FROM credit_card_debt_interlocks i
		LEFT JOIN accounts a ON a.id = i.account_id AND a.workspace_id = i.workspace_id
		WHERE i.workspace_id = $1
	`
	if targetMonth != nil {
		args = append(args, NormalizeYearMonth(*targetMonth))
		query += fmt.Sprintf(" AND i.target_month = $%d::date", len(args))
	}
	if status != nil {
		args = append(args, string(*status))
		query += fmt.Sprintf(" AND i.status = $%d", len(args))
	}
	query += `
		ORDER BY (CASE WHEN i.status = 'PENDING' THEN 0 ELSE 1 END) ASC,
		         i.target_month DESC,
		         i.created_at DESC,
		         i.id ASC
	`

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list credit card debt interlocks: %w", err)
	}
	defer rows.Close()

	items := make([]CCDebtInterlockView, 0)
	for rows.Next() {
		var v CCDebtInterlockView
		var statusStr string
		if err := rows.Scan(
			&v.ID, &v.WorkspaceID, &v.AccountID, &v.AccountName,
			&v.SourceMonth, &v.TargetMonth, &v.DeficitAmount, &statusStr, &v.ResolvedAt, &v.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan credit card debt interlock: %w", err)
		}
		v.Status = CCInterlockStatus(statusStr)
		v.DeficitAmount = roundMoney(v.DeficitAmount)
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating credit card debt interlocks: %w", err)
	}
	return items, nil
}

func (s *pgCCDebtInterlockService) ResolveInterlock(
	ctx context.Context,
	tx pgx.Tx,
	interlockID uuid.UUID,
	action CCInterlockResolutionAction,
) (*ResolveCCDebtInterlockResponse, error) {
	req := ResolveCCDebtInterlockRequest{Action: action}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}

	var il CreditCardDebtInterlock
	var statusStr string
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, account_id, source_month, target_month, deficit_amount, status, resolved_at, created_at
		FROM credit_card_debt_interlocks
		WHERE id = $1 AND workspace_id = $2
		FOR UPDATE
	`, interlockID, wsID).Scan(
		&il.ID, &il.WorkspaceID, &il.AccountID, &il.SourceMonth, &il.TargetMonth,
		&il.DeficitAmount, &statusStr, &il.ResolvedAt, &il.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to lock credit card debt interlock: %w", err)
	}
	il.Status = CCInterlockStatus(statusStr)
	il.DeficitAmount = roundMoney(il.DeficitAmount)

	if il.Status != CCInterlockPending {
		return nil, ErrInterlockAlreadyResolved
	}

	if action == CCInterlockActionKeepDeduction {
		var updatedStatus string
		if err := tx.QueryRow(ctx, `
			UPDATE credit_card_debt_interlocks
			SET status = 'RESOLVED_KEEP_DEDUCTION', resolved_at = NOW()
			WHERE id = $1 AND workspace_id = $2
			RETURNING status, resolved_at
		`, il.ID, wsID).Scan(&updatedStatus, &il.ResolvedAt); err != nil {
			return nil, fmt.Errorf("failed to resolve interlock as KEEP_DEDUCTION: %w", err)
		}
		il.Status = CCInterlockStatus(updatedStatus)
		return &ResolveCCDebtInterlockResponse{
			Interlock:             il,
			ReadyToAssignRestored: decimal.Zero.Round(2),
		}, nil
	}

	// Option B: CONVERT_TO_DEBT — move deficit onto the card's initial_debt_baseline and restore Ready to Assign.
	if il.AccountID == nil || *il.AccountID == uuid.Nil {
		return nil, ErrInterlockAccountMissing
	}

	var acctType string
	if err := tx.QueryRow(ctx, `
		SELECT type
		FROM accounts
		WHERE id = $1 AND workspace_id = $2
		FOR UPDATE
	`, *il.AccountID, wsID).Scan(&acctType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInterlockAccountMissing
		}
		return nil, fmt.Errorf("failed to lock interlock account: %w", err)
	}
	if acctType != "credit_card" {
		return nil, ErrInterlockNotCreditCard
	}

	var newBaseline decimal.Decimal
	if err := tx.QueryRow(ctx, `
		UPDATE accounts
		SET initial_debt_baseline = COALESCE(initial_debt_baseline, 0) + $3,
		    updated_at = NOW()
		WHERE id = $1 AND workspace_id = $2
		RETURNING initial_debt_baseline
	`, *il.AccountID, wsID, il.DeficitAmount).Scan(&newBaseline); err != nil {
		return nil, fmt.Errorf("failed to update card initial_debt_baseline: %w", err)
	}
	newBaseline = roundMoney(newBaseline)

	if _, err := tx.Exec(ctx, `
		UPDATE monthly_rollover_adjustments
		SET is_active = FALSE
		WHERE workspace_id = $1
		  AND interlock_id = $2
		  AND adjustment_type = 'CC_DEFICIT_DEDUCTION'
		  AND is_active = TRUE
	`, wsID, il.ID); err != nil {
		return nil, fmt.Errorf("failed to deactivate cc deficit adjustment: %w", err)
	}

	var updatedStatus string
	if err := tx.QueryRow(ctx, `
		UPDATE credit_card_debt_interlocks
		SET status = 'RESOLVED_CONVERT_TO_DEBT', resolved_at = NOW()
		WHERE id = $1 AND workspace_id = $2
		RETURNING status, resolved_at
	`, il.ID, wsID).Scan(&updatedStatus, &il.ResolvedAt); err != nil {
		return nil, fmt.Errorf("failed to resolve interlock as CONVERT_TO_DEBT: %w", err)
	}
	il.Status = CCInterlockStatus(updatedStatus)

	return &ResolveCCDebtInterlockResponse{
		Interlock:                  il,
		ReadyToAssignRestored:      il.DeficitAmount,
		UpdatedInitialDebtBaseline: &newBaseline,
	}, nil
}

// ListCCDebtInterlocks handles GET /api/v1/budget/cc-interlocks.
func (h *Handler) ListCCDebtInterlocks(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var monthPtr *time.Time
	if rawMonth := strings.TrimSpace(r.URL.Query().Get("month")); rawMonth != "" {
		ym, parseErr := parseMonthParam(rawMonth)
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, "invalid month format, expected YYYY-MM")
			return
		}
		monthPtr = &ym
	}

	var statusPtr *CCInterlockStatus
	if rawStatus := strings.TrimSpace(r.URL.Query().Get("status")); rawStatus != "" {
		st := CCInterlockStatus(rawStatus)
		if err := validateInterlockStatus(st); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		statusPtr = &st
	}

	var items []CCDebtInterlockView
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		items, txErr = h.interlockSvc.ListInterlocks(r.Context(), tx, monthPtr, statusPtr)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrInvalidInterlockStatus) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_list_cc_interlocks")
		return
	}

	writeJSON(w, http.StatusOK, items)
}

// ResolveCCDebtInterlock handles POST /api/v1/budget/cc-interlocks/{id}/resolve.
func (h *Handler) ResolveCCDebtInterlock(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	interlockID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_interlock_id")
		return
	}

	var req ResolveCCDebtInterlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := req.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var resp *ResolveCCDebtInterlockResponse
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		resp, txErr = h.interlockSvc.ResolveInterlock(r.Context(), tx, interlockID, req.Action)
		return txErr
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeErr(w, http.StatusNotFound, "cc_interlock_not_found")
		case errors.Is(err, ErrInterlockAlreadyResolved):
			writeErr(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrInvalidInterlockAction),
			errors.Is(err, ErrInterlockAccountMissing),
			errors.Is(err, ErrInterlockNotCreditCard):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, "failed_to_resolve_cc_interlock")
		}
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
