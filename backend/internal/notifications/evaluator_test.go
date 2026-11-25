package notifications

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func TestComputeBurnRatio(t *testing.T) {
	cases := []struct {
		name     string
		spent    string
		budget   string
		wantOver bool
	}{
		{"below threshold 79.99%", "79.99", "100.00", false},
		{"exact threshold 80.00%", "80.00", "100.00", true},
		{"above threshold 82%", "82.00", "100.00", true},
		{"overspent 125%", "125.00", "100.00", true},
		{"zero budget does not alert", "50.00", "0.00", false},
		{"negative budget does not alert", "50.00", "-10.00", false},
		{"zero spend does not alert", "0.00", "100.00", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spent := decimal.RequireFromString(tc.spent)
			budget := decimal.RequireFromString(tc.budget)
			_, got := ComputeBurnRatio(spent, budget)
			if got != tc.wantOver {
				t.Fatalf("ComputeBurnRatio(%s, %s) exceeded = %v, want %v", tc.spent, tc.budget, got, tc.wantOver)
			}
		})
	}
}

func TestDaysUntilStatementDue(t *testing.T) {
	cases := []struct {
		name   string
		now    time.Time
		dueDay int
		want   int
		wantOK bool
	}{
		{"invalid day 0", time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), 0, 0, false},
		{"invalid day 32", time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), 32, 0, false},
		{"due today", time.Date(2026, 5, 10, 15, 30, 0, 0, time.UTC), 10, 0, true},
		{"due in 5 days", time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC), 15, 5, true},
		{"due in 6 days", time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC), 16, 6, true},
		{"month wrap", time.Date(2026, 5, 29, 8, 0, 0, 0, time.UTC), 2, 4, true},
		{"year wrap", time.Date(2026, 12, 29, 8, 0, 0, 0, time.UTC), 2, 4, true},
		{"non-leap feb clamp 31->28", time.Date(2026, 2, 25, 8, 0, 0, 0, time.UTC), 31, 3, true},
		{"leap feb clamp 31->29", time.Date(2024, 2, 25, 8, 0, 0, 0, time.UTC), 31, 4, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DaysUntilStatementDue(tc.now, tc.dueDay)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("DaysUntilStatementDue(%v, %d) = (%d, %v), want (%d, %v)", tc.now, tc.dueDay, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestEvaluateStatementDueCandidates(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	dueIn3 := 13
	dueIn5 := 15
	dueIn6 := 16

	accounts := []account.Account{
		{
			ID:             uuid.New(),
			Name:           "Main Checking",
			Type:           account.Checking,
			CurrentBalance: decimal.RequireFromString("500.00"),
			IsOnBudget:     true,
		},
		{
			ID:             uuid.New(),
			Name:           "Off-Budget Checking",
			Type:           account.Checking,
			CurrentBalance: decimal.RequireFromString("5000.00"),
			IsOnBudget:     false,
		},
		{
			ID:              uuid.New(),
			Name:            "Chase Sapphire",
			Type:            account.CreditCard,
			CardMode:        account.PayInFull,
			IsOnBudget:      true,
			CurrentBalance:  decimal.RequireFromString("-650.00"),
			StatementDueDay: &dueIn3,
		},
		{
			ID:              uuid.New(),
			Name:            "Covered Card",
			Type:            account.CreditCard,
			CardMode:        account.PayInFull,
			IsOnBudget:      true,
			CurrentBalance:  decimal.RequireFromString("-450.00"),
			StatementDueDay: &dueIn5,
		},
		{
			ID:              uuid.New(),
			Name:            "Too Far Out",
			Type:            account.CreditCard,
			CardMode:        account.PayInFull,
			IsOnBudget:      true,
			CurrentBalance:  decimal.RequireFromString("-900.00"),
			StatementDueDay: &dueIn6,
		},
		{
			ID:              uuid.New(),
			Name:            "Revolver Card",
			Type:            account.CreditCard,
			CardMode:        account.DebtPaydown,
			IsOnBudget:      true,
			CurrentBalance:  decimal.RequireFromString("-2000.00"),
			StatementDueDay: &dueIn3,
		},
		{
			ID:              uuid.New(),
			Name:            "No Due Day",
			Type:            account.CreditCard,
			CardMode:        account.PayInFull,
			IsOnBudget:      true,
			CurrentBalance:  decimal.RequireFromString("-900.00"),
			StatementDueDay: nil,
		},
	}

	cands := EvaluateStatementDueCandidates(accounts, now)
	if len(cands) != 1 {
		t.Fatalf("expected 1 statement alert candidate, got %d: %+v", len(cands), cands)
	}
	if cands[0].AccountName != "Chase Sapphire" || cands[0].DaysUntilDue != 3 {
		t.Fatalf("unexpected candidate: %+v", cands[0])
	}
	if cands[0].CheckingBalance.StringFixed(2) != "500.00" || cands[0].StatementBalance.StringFixed(2) != "650.00" {
		t.Fatalf("unexpected balances in candidate: %+v", cands[0])
	}
}

type recordingPushService struct {
	enabled bool
	mu      sync.Mutex
	sent    []NotificationPayload
	sendErr error
}

func (r *recordingPushService) Enabled() bool     { return r.enabled }
func (r *recordingPushService) PublicKey() string { return "pub" }
func (r *recordingPushService) Subscribe(context.Context, uuid.UUID, PushSubscriptionDTO) error {
	return nil
}
func (r *recordingPushService) Unsubscribe(context.Context, uuid.UUID, string) error {
	return nil
}
func (r *recordingPushService) SendNotification(_ context.Context, _ uuid.UUID, payload NotificationPayload) error {
	if r.sendErr != nil {
		return r.sendErr
	}
	r.mu.Lock()
	r.sent = append(r.sent, payload)
	r.mu.Unlock()
	return nil
}

func TestEvaluator_SpendingVelocityAndCooldownIntegration(t *testing.T) {
	adminPool, appPool := openTestPools(t)
	ctx := context.Background()

	wsID := uuid.New()
	userID := uuid.New()
	userB := uuid.New()

	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Notif Eval WS')", wsID); err != nil {
		t.Fatalf("failed to seed workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to seed member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM push_subscriptions WHERE user_id IN ($1, $2)", userID, userB)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	var checkingID, offBudgetID, catDiningID, catGroceryID uuid.UUID
	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, 'Checking', 'checking', 2000.00, TRUE)
			RETURNING id
		`, wsID).Scan(&checkingID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, 'OffBudget', 'checking', 5000.00, FALSE)
			RETURNING id
		`, wsID).Scan(&offBudgetID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, sort_order)
			VALUES ($1, 'Dining', 'Fun', 100.00, 1)
			RETURNING id
		`, wsID).Scan(&catDiningID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, sort_order)
			VALUES ($1, 'Groceries', 'Food', 100.00, 2)
			RETURNING id
		`, wsID).Scan(&catGroceryID); err != nil {
			return err
		}

		// 1) Dining: $82.00 spend on on-budget checking (82% -> triggers).
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
			VALUES ($1, $2, $3, 'MANUAL_OFFLINE', 'POSTED', -82.00, 'Bistro', '2026-05-10', TRUE)
		`, wsID, checkingID, catDiningID); err != nil {
			return err
		}

		// 2) Groceries: $90 on off-budget account + $90 linked transfer on on-budget account (must be excluded!).
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
			VALUES ($1, $2, $3, 'MANUAL_OFFLINE', 'POSTED', -90.00, 'OffBudget Store', '2026-05-10', TRUE)
		`, wsID, offBudgetID, catGroceryID); err != nil {
			return err
		}
		txA := uuid.New()
		txB := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (id, workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
			VALUES ($1, $2, $3, $4, 'BANK_FEED', 'POSTED', -90.00, 'Xfer Out', '2026-05-10', TRUE),
			       ($5, $2, $6, NULL, 'BANK_FEED', 'POSTED', 90.00, 'Xfer In', '2026-05-10', TRUE)
		`, txA, wsID, checkingID, catGroceryID, txB, offBudgetID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE transactions SET transfer_linked_transaction_id = $2 WHERE id = $1`, txA, txB); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to seed test data: %v", err)
	}

	// Real WebPushService against mock httptest server to verify encrypted delivery end-to-end.
	var mu sync.Mutex
	var pushDeliveries int
	pushSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pushDeliveries++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer pushSrv.Close()

	cfg := generateTestVAPIDConfig(t, "mailto:alerts@example.com")
	pushSvc := NewWebPushService(appPool, cfg, pushSrv.Client())
	p256dh, auth := generateTestClientKeys(t)
	if err := pushSvc.Subscribe(ctx, userID, PushSubscriptionDTO{
		Endpoint: pushSrv.URL + "/device1",
		P256dh:   p256dh,
		Auth:     auth,
	}); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	currentTime := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	eval := NewEvaluator(appPool, pushSvc).WithClock(func() time.Time { return currentTime })

	// Cross-tenant isolation: userB evaluating wsID must send 0 alerts.
	resB, err := eval.EvaluateWorkspace(ctx, wsID, userB)
	if err != nil {
		t.Fatalf("cross-tenant EvaluateWorkspace returned error: %v", err)
	}
	if resB.TotalSent() != 0 {
		t.Fatalf("cross-tenant EvaluateWorkspace sent %d alerts, want 0", resB.TotalSent())
	}

	// Disabled push service must no-op cleanly.
	disabledEval := NewEvaluator(appPool, NewWebPushService(appPool, VAPIDConfig{}, pushSrv.Client())).
		WithClock(func() time.Time { return currentTime })
	resDisabled, err := disabledEval.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil || resDisabled.TotalSent() != 0 {
		t.Fatalf("disabled EvaluateWorkspace = (%+v, %v), want zero/nil", resDisabled, err)
	}

	// First evaluation at 82% Dining spend -> dispatches 1 spending alert.
	res1, err := eval.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil {
		t.Fatalf("EvaluateWorkspace #1 failed: %v", err)
	}
	if res1.SpendingAlertsSent != 1 || res1.StatementAlertsSent != 0 || res1.SuppressedByCooldown != 0 {
		t.Fatalf("unexpected res1: %+v", res1)
	}

	// Second charge 1 hour later -> suppressed by 24h cooldown.
	currentTime = currentTime.Add(1 * time.Hour)
	if err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
			VALUES ($1, $2, $3, 'MANUAL_OFFLINE', 'POSTED', -10.00, 'Cafe', '2026-05-10', TRUE)
		`, wsID, checkingID, catDiningID)
		return err
	}); err != nil {
		t.Fatalf("failed to insert second charge: %v", err)
	}

	res2, err := eval.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil {
		t.Fatalf("EvaluateWorkspace #2 failed: %v", err)
	}
	if res2.SpendingAlertsSent != 0 || res2.SuppressedByCooldown != 1 {
		t.Fatalf("expected cooldown suppression after 1h, got %+v", res2)
	}

	// 24 hours + 1 minute after original send -> cooldown expired -> dispatches again.
	currentTime = time.Date(2026, 5, 11, 12, 1, 0, 0, time.UTC)
	res3, err := eval.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil {
		t.Fatalf("EvaluateWorkspace #3 failed: %v", err)
	}
	if res3.SpendingAlertsSent != 1 || res3.SuppressedByCooldown != 0 {
		t.Fatalf("expected alert after 24h cooldown expiry, got %+v", res3)
	}

	mu.Lock()
	deliveries := pushDeliveries
	mu.Unlock()
	if deliveries != 2 {
		t.Fatalf("expected 2 encrypted push deliveries total, got %d", deliveries)
	}
}

func TestEvaluator_StatementDueAndFailureReleaseIntegration(t *testing.T) {
	adminPool, appPool := openTestPools(t)
	ctx := context.Background()

	wsID := uuid.New()
	userID := uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Statement Eval WS')", wsID); err != nil {
		t.Fatalf("failed to seed workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to seed member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	var checkingID, cardID uuid.UUID
	dueDay := 14
	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, 'Primary Checking', 'checking', 300.00, TRUE)
			RETURNING id
		`, wsID).Scan(&checkingID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget, card_mode, statement_due_day)
			VALUES ($1, 'Amex Gold', 'credit_card', -550.00, TRUE, 'PAY_IN_FULL', $2)
			RETURNING id
		`, wsID, dueDay).Scan(&cardID)
	})
	if err != nil {
		t.Fatalf("failed to seed accounts: %v", err)
	}

	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)

	// 1) Total push delivery failure releases cooldown so retry succeeds.
	failingPush := &recordingPushService{enabled: true, sendErr: errors.New("push endpoint 502")}
	failingEval := NewEvaluator(appPool, failingPush).WithClock(func() time.Time { return now })
	resFail, err := failingEval.EvaluateWorkspace(ctx, wsID, userID)
	if err == nil {
		t.Fatalf("expected push delivery error")
	}
	if resFail.TotalSent() != 0 {
		t.Fatalf("expected 0 sent on failure, got %+v", resFail)
	}

	// Verify cooldown row was rolled back/deleted.
	var cooldownCount int
	if err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT COUNT(*) FROM notification_cooldowns WHERE workspace_id = $1", wsID).Scan(&cooldownCount)
	}); err != nil {
		t.Fatalf("failed to count cooldowns: %v", err)
	}
	if cooldownCount != 0 {
		t.Fatalf("expected cooldown release after total push failure, got %d rows", cooldownCount)
	}

	// 2) Next attempt with healthy push service dispatches statement due alert.
	healthyPush := &recordingPushService{enabled: true}
	healthyEval := NewEvaluator(appPool, healthyPush).WithClock(func() time.Time { return now })
	resOK, err := healthyEval.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil {
		t.Fatalf("healthy EvaluateWorkspace failed: %v", err)
	}
	if resOK.StatementAlertsSent != 1 || len(healthyPush.sent) != 1 {
		t.Fatalf("expected 1 statement alert sent, got res=%+v sent=%d", resOK, len(healthyPush.sent))
	}

	// 3) Within 24h cooldown -> suppressed.
	healthyEvalLater := NewEvaluator(appPool, healthyPush).WithClock(func() time.Time { return now.Add(2 * time.Hour) })
	resSuppressed, err := healthyEvalLater.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil {
		t.Fatalf("suppressed EvaluateWorkspace failed: %v", err)
	}
	if resSuppressed.StatementAlertsSent != 0 || resSuppressed.SuppressedByCooldown != 1 {
		t.Fatalf("expected suppression within 24h, got %+v", resSuppressed)
	}

	// 4) Once checking covers statement balance after cooldown expires -> 0 alerts.
	if err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE accounts SET current_balance = 600.00 WHERE id = $1", checkingID)
		return err
	}); err != nil {
		t.Fatalf("failed to top up checking: %v", err)
	}

	healthyEvalCovered := NewEvaluator(appPool, healthyPush).WithClock(func() time.Time { return now.Add(25 * time.Hour) })
	resCovered, err := healthyEvalCovered.EvaluateWorkspace(ctx, wsID, userID)
	if err != nil {
		t.Fatalf("covered EvaluateWorkspace failed: %v", err)
	}
	if resCovered.TotalSent() != 0 || resCovered.SuppressedByCooldown != 0 {
		t.Fatalf("expected no alert once checking >= statement, got %+v", resCovered)
	}
}
