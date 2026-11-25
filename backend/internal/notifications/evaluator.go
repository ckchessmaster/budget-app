package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const (
	TriggerSpendingVelocity = "SPENDING_VELOCITY"
	TriggerStatementDue     = "STATEMENT_DUE"
	DefaultCooldownWindow   = 24 * time.Hour
	StatementDueWindowDays  = 5
)

var SpendingVelocityThreshold = decimal.RequireFromString("0.80")

type EvaluationResult struct {
	SpendingAlertsSent   int `json:"spending_alerts_sent"`
	StatementAlertsSent  int `json:"statement_alerts_sent"`
	SuppressedByCooldown int `json:"suppressed_by_cooldown"`
}

func (r EvaluationResult) TotalSent() int {
	return r.SpendingAlertsSent + r.StatementAlertsSent
}

type Evaluator interface {
	EvaluateWorkspace(ctx context.Context, workspaceID, userID uuid.UUID) (EvaluationResult, error)
}

type SpendingAlertCandidate struct {
	CategoryID   uuid.UUID
	CategoryName string
	TotalBudget  decimal.Decimal
	TotalSpent   decimal.Decimal
	BurnRatio    decimal.Decimal
}

type StatementAlertCandidate struct {
	AccountID        uuid.UUID
	AccountName      string
	StatementBalance decimal.Decimal
	CheckingBalance  decimal.Decimal
	DaysUntilDue     int
}

type NotificationEvaluator struct {
	pool           *pgxpool.Pool
	push           PushService
	cooldownWindow time.Duration
	nowFn          func() time.Time
}

func NewEvaluator(pool *pgxpool.Pool, push PushService) *NotificationEvaluator {
	return &NotificationEvaluator{
		pool:           pool,
		push:           push,
		cooldownWindow: DefaultCooldownWindow,
		nowFn: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (e *NotificationEvaluator) WithClock(nowFn func() time.Time) *NotificationEvaluator {
	if nowFn != nil {
		e.nowFn = nowFn
	}
	return e
}

// ComputeBurnRatio computes spent / totalBudget and reports whether it meets or
// exceeds SpendingVelocityThreshold (0.80).
func ComputeBurnRatio(spent, totalBudget decimal.Decimal) (decimal.Decimal, bool) {
	if !totalBudget.IsPositive() || !spent.IsPositive() {
		return decimal.Zero, false
	}
	ratio := spent.Div(totalBudget)
	return ratio, spent.GreaterThanOrEqual(totalBudget.Mul(SpendingVelocityThreshold))
}

// DaysUntilStatementDue calculates the number of days from today's UTC date until
// the next occurrence of dueDay (1..31), clamping to the last day of shorter months.
func DaysUntilStatementDue(now time.Time, dueDay int) (int, bool) {
	if dueDay < 1 || dueDay > 31 {
		return 0, false
	}

	u := now.UTC()
	today := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)

	clampedForMonth := func(year int, month time.Month) time.Time {
		daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		day := dueDay
		if day > daysInMonth {
			day = daysInMonth
		}
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}

	candidate := clampedForMonth(today.Year(), today.Month())
	if candidate.Before(today) {
		nextMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		candidate = clampedForMonth(nextMonth.Year(), nextMonth.Month())
	}

	return int(candidate.Sub(today).Hours() / 24), true
}

func EvaluateSpendingVelocityCandidates(categories []budget.CategoryBudgetView) []SpendingAlertCandidate {
	var candidates []SpendingAlertCandidate
	for _, cat := range categories {
		totalBudget := cat.CarryoverBalance.Add(cat.EffectiveTarget).Round(2)
		totalSpent := cat.SpentCleared.Add(cat.SpentPending).Round(2)
		ratio, exceeded := ComputeBurnRatio(totalSpent, totalBudget)
		if !exceeded {
			continue
		}
		candidates = append(candidates, SpendingAlertCandidate{
			CategoryID:   cat.ID,
			CategoryName: cat.Name,
			TotalBudget:  totalBudget,
			TotalSpent:   totalSpent,
			BurnRatio:    ratio,
		})
	}
	return candidates
}

func EvaluateStatementDueCandidates(accounts []account.Account, now time.Time) []StatementAlertCandidate {
	checkingBalance := decimal.Zero
	for _, a := range accounts {
		if a.Type == account.Checking && a.IsOnBudget {
			checkingBalance = checkingBalance.Add(a.CurrentBalance)
		}
	}
	checkingBalance = checkingBalance.Round(2)

	var candidates []StatementAlertCandidate
	for _, a := range accounts {
		if a.Type != account.CreditCard || !a.IsOnBudget || a.CardMode != account.PayInFull || a.StatementDueDay == nil || !a.CurrentBalance.IsNegative() {
			continue
		}
		statementBalance := a.CurrentBalance.Abs().Round(2)
		daysUntil, ok := DaysUntilStatementDue(now, *a.StatementDueDay)
		if !ok || daysUntil > StatementDueWindowDays {
			continue
		}
		if checkingBalance.LessThan(statementBalance) {
			candidates = append(candidates, StatementAlertCandidate{
				AccountID:        a.ID,
				AccountName:      a.Name,
				StatementBalance: statementBalance,
				CheckingBalance:  checkingBalance,
				DaysUntilDue:     daysUntil,
			})
		}
	}
	return candidates
}

func buildSpendingAlertPayload(c SpendingAlertCandidate) NotificationPayload {
	pct := c.BurnRatio.Mul(decimal.NewFromInt(100)).Round(0)
	return NotificationPayload{
		Title: fmt.Sprintf("Spending Alert: %s", c.CategoryName),
		Body: fmt.Sprintf(
			"You've spent $%s of $%s (%s%%) in %s this month.",
			c.TotalSpent.StringFixed(2),
			c.TotalBudget.StringFixed(2),
			pct.String(),
			c.CategoryName,
		),
		URL: "/budget",
	}
}

func buildStatementAlertPayload(c StatementAlertCandidate) NotificationPayload {
	duePhrase := fmt.Sprintf("due in %d days", c.DaysUntilDue)
	switch c.DaysUntilDue {
	case 0:
		duePhrase = "due today"
	case 1:
		duePhrase = "due in 1 day"
	}
	return NotificationPayload{
		Title: fmt.Sprintf("Statement Due Soon: %s", c.AccountName),
		Body: fmt.Sprintf(
			"%s statement ($%s) is %s, exceeding checking balance ($%s).",
			c.AccountName,
			c.StatementBalance.StringFixed(2),
			duePhrase,
			c.CheckingBalance.StringFixed(2),
		),
		URL: "/accounts",
	}
}

type pendingAlert struct {
	triggerType string
	entityID    uuid.UUID
	payload     NotificationPayload
}

func (e *NotificationEvaluator) EvaluateWorkspace(ctx context.Context, workspaceID, userID uuid.UUID) (EvaluationResult, error) {
	if e == nil || e.pool == nil || e.push == nil || !e.push.Enabled() || userID == uuid.Nil {
		return EvaluationResult{}, nil
	}

	now := e.nowFn().UTC()
	ym := budget.NormalizeYearMonth(now)
	nextYM := ym.AddDate(0, 1, 0)

	var (
		categories []budget.CategoryBudgetView
		accounts   []account.Account
		recipients []uuid.UUID
		visible    bool
	)

	err := db.WithWorkspaceTx(ctx, e.pool, userID, func(tx pgx.Tx) error {
		if workspaceID == uuid.Nil {
			resolved, err := db.ActiveWorkspaceID(ctx, tx)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				return err
			}
			workspaceID = resolved
			visible = true
		} else {
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = $1)
			`, workspaceID).Scan(&visible); err != nil {
				return err
			}
			if !visible {
				return nil
			}
		}

		catRows, err := tx.Query(ctx, `
			WITH cat_realloc AS (
				SELECT c.id AS category_id,
				       COALESCE(SUM(CASE WHEN r.to_category_id = c.id THEN r.amount ELSE 0 END), 0) -
				       COALESCE(SUM(CASE WHEN r.from_category_id = c.id THEN r.amount ELSE 0 END), 0) AS net_transfer
				FROM categories c
				LEFT JOIN budget_reallocations r
				       ON r.workspace_id = $3
				      AND r.year_month = $1::date
				      AND (r.to_category_id = c.id OR r.from_category_id = c.id)
				WHERE c.workspace_id = $3
				GROUP BY c.id
			),
			cat_spend AS (
				SELECT t.category_id,
				       COALESCE(SUM(CASE WHEN t.status = 'POSTED' THEN -t.amount ELSE 0 END), 0) AS spent_cleared,
				       COALESCE(SUM(CASE WHEN t.status = 'PENDING' THEN -t.amount ELSE 0 END), 0) AS spent_pending
				FROM transactions t
				INNER JOIN accounts a ON a.id = t.account_id
				WHERE t.workspace_id = $3
				  AND a.workspace_id = $3
				  AND t.category_id IS NOT NULL
				  AND t.is_transfer = FALSE
				  AND a.is_on_budget = TRUE
				  AND t.transaction_date >= $1::date
				  AND t.transaction_date < $2::date
				GROUP BY t.category_id
			)
			SELECT c.id,
			       c.name,
			       COALESCE(mcb.original_planned, c.monthly_budget) AS original_planned,
			       COALESCE(mcb.carryover_balance, 0) AS carryover_balance,
			       COALESCE(cr.net_transfer, 0) AS net_transfer,
			       COALESCE(cs.spent_cleared, 0) AS spent_cleared,
			       COALESCE(cs.spent_pending, 0) AS spent_pending
			FROM categories c
			LEFT JOIN monthly_category_budgets mcb
			       ON mcb.workspace_id = $3
			      AND mcb.category_id = c.id
			      AND mcb.year_month = $1::date
			LEFT JOIN cat_realloc cr ON cr.category_id = c.id
			LEFT JOIN cat_spend cs ON cs.category_id = c.id
			WHERE c.workspace_id = $3
			ORDER BY c.sort_order ASC, c.name ASC
		`, ym, nextYM, workspaceID)
		if err != nil {
			return fmt.Errorf("failed to load category budgets for notification evaluation: %w", err)
		}
		defer catRows.Close()

		for catRows.Next() {
			var v budget.CategoryBudgetView
			var netTransfer decimal.Decimal
			if err := catRows.Scan(
				&v.ID,
				&v.Name,
				&v.OriginalPlanned,
				&v.CarryoverBalance,
				&netTransfer,
				&v.SpentCleared,
				&v.SpentPending,
			); err != nil {
				return fmt.Errorf("failed to scan category budget row: %w", err)
			}
			v.CarryoverBalance = v.CarryoverBalance.Round(2)
			v.EffectiveTarget = v.OriginalPlanned.Add(netTransfer).Round(2)
			categories = append(categories, v)
		}
		if err := catRows.Err(); err != nil {
			return fmt.Errorf("error iterating category budget rows: %w", err)
		}

		acctRows, err := tx.Query(ctx, `
			SELECT id, workspace_id, name, type, current_balance, is_on_budget, card_mode, statement_due_day
			FROM accounts
			WHERE workspace_id = $1
			ORDER BY type ASC, name ASC
		`, workspaceID)
		if err != nil {
			return fmt.Errorf("failed to load accounts for notification evaluation: %w", err)
		}
		defer acctRows.Close()

		for acctRows.Next() {
			var a account.Account
			var accType, cardMode string
			if err := acctRows.Scan(
				&a.ID,
				&a.WorkspaceID,
				&a.Name,
				&accType,
				&a.CurrentBalance,
				&a.IsOnBudget,
				&cardMode,
				&a.StatementDueDay,
			); err != nil {
				return fmt.Errorf("failed to scan account row: %w", err)
			}
			a.Type = account.AccountType(accType)
			a.CardMode = account.CCMode(cardMode)
			accounts = append(accounts, a)
		}
		if err := acctRows.Err(); err != nil {
			return fmt.Errorf("error iterating account rows: %w", err)
		}

		memberRows, err := tx.Query(ctx, `
			SELECT user_id
			FROM workspace_members
			WHERE workspace_id = $1
			ORDER BY created_at ASC
		`, workspaceID)
		if err != nil {
			return fmt.Errorf("failed to load workspace recipients: %w", err)
		}
		defer memberRows.Close()

		for memberRows.Next() {
			var memberID uuid.UUID
			if err := memberRows.Scan(&memberID); err != nil {
				return fmt.Errorf("failed to scan workspace recipient: %w", err)
			}
			recipients = append(recipients, memberID)
		}
		if err := memberRows.Err(); err != nil {
			return fmt.Errorf("error iterating workspace recipients: %w", err)
		}

		return nil
	})
	if err != nil {
		return EvaluationResult{}, err
	}
	if !visible || workspaceID == uuid.Nil {
		return EvaluationResult{}, nil
	}
	if len(recipients) == 0 {
		recipients = []uuid.UUID{userID}
	}

	var queue []pendingAlert
	for _, cand := range EvaluateSpendingVelocityCandidates(categories) {
		queue = append(queue, pendingAlert{
			triggerType: TriggerSpendingVelocity,
			entityID:    cand.CategoryID,
			payload:     buildSpendingAlertPayload(cand),
		})
	}
	for _, cand := range EvaluateStatementDueCandidates(accounts, now) {
		queue = append(queue, pendingAlert{
			triggerType: TriggerStatementDue,
			entityID:    cand.AccountID,
			payload:     buildStatementAlertPayload(cand),
		})
	}

	var result EvaluationResult
	var allErrs []error

	for _, item := range queue {
		claimed, prevSentAt, err := e.claimCooldown(ctx, workspaceID, userID, item.triggerType, item.entityID, now)
		if err != nil {
			allErrs = append(allErrs, err)
			continue
		}
		if !claimed {
			result.SuppressedByCooldown++
			continue
		}

		succeededCount := 0
		var sendErrs []error
		for _, recipientID := range recipients {
			if err := e.push.SendNotification(ctx, recipientID, item.payload); err != nil {
				sendErrs = append(sendErrs, err)
			} else {
				succeededCount++
			}
		}

		if succeededCount == 0 && len(sendErrs) > 0 {
			if relErr := e.releaseCooldown(ctx, workspaceID, userID, item.triggerType, item.entityID, now, prevSentAt); relErr != nil {
				sendErrs = append(sendErrs, relErr)
			}
		} else if succeededCount > 0 {
			switch item.triggerType {
			case TriggerSpendingVelocity:
				result.SpendingAlertsSent++
			case TriggerStatementDue:
				result.StatementAlertsSent++
			}
		}

		if len(sendErrs) > 0 {
			allErrs = append(allErrs, sendErrs...)
		}
	}

	return result, errors.Join(allErrs...)
}

func (e *NotificationEvaluator) claimCooldown(
	ctx context.Context,
	workspaceID, userID uuid.UUID,
	triggerType string,
	entityID uuid.UUID,
	now time.Time,
) (claimed bool, prevSentAt *time.Time, err error) {
	cutoff := now.Add(-e.cooldownWindow)

	err = db.WithWorkspaceTx(ctx, e.pool, userID, func(tx pgx.Tx) error {
		var existingSentAt time.Time
		scanErr := tx.QueryRow(ctx, `
			SELECT last_sent_at
			FROM notification_cooldowns
			WHERE workspace_id = $1 AND trigger_type = $2 AND entity_id = $3
			FOR UPDATE
		`, workspaceID, triggerType, entityID).Scan(&existingSentAt)
		if scanErr == nil {
			if existingSentAt.After(cutoff) {
				claimed = false
				return nil
			}
			if _, updErr := tx.Exec(ctx, `
				UPDATE notification_cooldowns
				SET last_sent_at = $4
				WHERE workspace_id = $1 AND trigger_type = $2 AND entity_id = $3
			`, workspaceID, triggerType, entityID, now); updErr != nil {
				return updErr
			}
			prevCopy := existingSentAt
			prevSentAt = &prevCopy
			claimed = true
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}

		var insertedID uuid.UUID
		insErr := tx.QueryRow(ctx, `
			INSERT INTO notification_cooldowns (workspace_id, trigger_type, entity_id, last_sent_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (workspace_id, trigger_type, entity_id) DO UPDATE
			SET last_sent_at = EXCLUDED.last_sent_at
			WHERE notification_cooldowns.last_sent_at <= $5
			RETURNING id
		`, workspaceID, triggerType, entityID, now, cutoff).Scan(&insertedID)
		if insErr != nil {
			if errors.Is(insErr, pgx.ErrNoRows) {
				claimed = false
				return nil
			}
			return insErr
		}

		claimed = true
		return nil
	})
	return claimed, prevSentAt, err
}

func (e *NotificationEvaluator) releaseCooldown(
	ctx context.Context,
	workspaceID, userID uuid.UUID,
	triggerType string,
	entityID uuid.UUID,
	claimedAt time.Time,
	prevSentAt *time.Time,
) error {
	return db.WithWorkspaceTx(ctx, e.pool, userID, func(tx pgx.Tx) error {
		if prevSentAt == nil {
			_, err := tx.Exec(ctx, `
				DELETE FROM notification_cooldowns
				WHERE workspace_id = $1 AND trigger_type = $2 AND entity_id = $3 AND last_sent_at = $4
			`, workspaceID, triggerType, entityID, claimedAt)
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE notification_cooldowns
			SET last_sent_at = $5
			WHERE workspace_id = $1 AND trigger_type = $2 AND entity_id = $3 AND last_sent_at = $4
		`, workspaceID, triggerType, entityID, claimedAt, *prevSentAt)
		return err
	})
}
