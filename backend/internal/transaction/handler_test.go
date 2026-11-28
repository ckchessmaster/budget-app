package transaction

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type stubPushService struct {
	mu      sync.Mutex
	enabled bool
	sent    []notifications.NotificationPayload
}

func (s *stubPushService) Enabled() bool     { return s.enabled }
func (s *stubPushService) PublicKey() string { return "pub" }
func (s *stubPushService) Subscribe(context.Context, uuid.UUID, notifications.PushSubscriptionDTO) error {
	return nil
}
func (s *stubPushService) Unsubscribe(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *stubPushService) SendNotification(_ context.Context, _ uuid.UUID, payload notifications.NotificationPayload) error {
	s.mu.Lock()
	s.sent = append(s.sent, payload)
	s.mu.Unlock()
	return nil
}

type failingEvaluator struct{}

func (f failingEvaluator) EvaluateWorkspace(context.Context, uuid.UUID, uuid.UUID) (notifications.EvaluationResult, error) {
	return notifications.EvaluationResult{}, errors.New("evaluator unavailable")
}

func TestTransactionHandler_NotificationHooks(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)
	ctx := context.Background()

	var checkingID, catID uuid.UUID
	if err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		acctRepo := account.NewRepo()
		acct := &account.Account{
			WorkspaceID:    wsID,
			Name:           "Checking",
			Type:           account.Checking,
			CurrentBalance: decimal.RequireFromString("2000.00"),
			IsOnBudget:     true,
		}
		if err := acctRepo.Create(ctx, tx, acct); err != nil {
			return err
		}
		checkingID = acct.ID

		return tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, sort_order)
			VALUES ($1, 'Dining', 'Fun', 100.00, 1)
			RETURNING id
		`, wsID).Scan(&catID)
	}); err != nil {
		t.Fatalf("failed to seed account/category: %v", err)
	}

	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")

	pushStub := &stubPushService{enabled: true}
	eval := notifications.NewEvaluator(appPool, pushStub).WithClock(func() time.Time { return now })

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			reqCtx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			reqCtx = middleware.WithWorkspaceContext(reqCtx, middleware.WorkspaceContext{
				WorkspaceID: wsID,
				UserID:      userID,
				Role:        middleware.RoleOwner,
			})
			reqCtx = db.WithWorkspaceID(reqCtx, wsID)
			next.ServeHTTP(w, req.WithContext(reqCtx))
		})
	})
	replayStub := &recordingReplay{id: "offline-replay-test"}
	RegisterRoutes(r, appPool, replayStub, eval)

	// 1. POST /api/v1/transactions crossing 80% triggers 1 alert
	body1 := `{"account_id":"` + checkingID.String() + `","total_amount":"-85.00","merchant_raw":"Bistro","category_id":"` + catID.String() + `","transaction_date":"` + now.Format(time.RFC3339) + `"}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(body1))
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/transactions status = %d body=%s, want 201", rec1.Code, rec1.Body.String())
	}
	if len(pushStub.sent) != 1 {
		t.Fatalf("expected 1 push alert after POST crossing 80%%, got %d", len(pushStub.sent))
	}

	// Extract created transaction ID for PATCH test
	var txID string
	if err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT id::text FROM transactions WHERE workspace_id = $1 LIMIT 1", wsID).Scan(&txID)
	}); err != nil {
		t.Fatalf("failed to load created tx id: %v", err)
	}

	// 2. PATCH /api/v1/transactions/{id} invokes evaluator; cooldown suppresses second alert
	patchBody := `{"amount":"-95.00"}`
	reqPatch := httptest.NewRequest(http.MethodPatch, "/api/v1/transactions/"+txID, strings.NewReader(patchBody))
	recPatch := httptest.NewRecorder()
	r.ServeHTTP(recPatch, reqPatch)
	if recPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /api/v1/transactions/{id} status = %d body=%s, want 200", recPatch.Code, recPatch.Body.String())
	}
	if len(pushStub.sent) != 1 {
		t.Fatalf("expected cooldown to suppress alert on PATCH, got %d total alerts", len(pushStub.sent))
	}

	// 3. Reset cooldown, then POST /api/v1/transactions/sync. The batch is handed to the
	//    replay workflow, which owns the notification pass now; the request itself must not
	//    evaluate alerts, because it no longer knows whether any item landed.
	if err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM notification_cooldowns WHERE workspace_id = $1", wsID)
		return err
	}); err != nil {
		t.Fatalf("failed to clear cooldowns: %v", err)
	}

	syncBody := `{"items":[{"local_id":"loc-1","account_id":"` + checkingID.String() + `","amount":"-5.00","merchant_raw":"Coffee","category_id":"` + catID.String() + `","transaction_date":"` + todayStr + `"}]}`
	reqSync := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/sync", strings.NewReader(syncBody))
	recSync := httptest.NewRecorder()
	r.ServeHTTP(recSync, reqSync)
	if recSync.Code != http.StatusAccepted {
		t.Fatalf("POST /api/v1/transactions/sync status = %d body=%s, want 202", recSync.Code, recSync.Body.String())
	}
	if replayStub.itemCount() != 1 {
		t.Fatalf("sync handed %d items to the replay workflow, want 1", replayStub.itemCount())
	}
	if len(pushStub.sent) != 1 {
		t.Fatalf("sync must not evaluate notifications inline any more; got %d alerts", len(pushStub.sent))
	}

	// 4. Evaluator failure never fails transaction creation HTTP response
	rFail := chi.NewRouter()
	rFail.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			reqCtx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			reqCtx = middleware.WithWorkspaceContext(reqCtx, middleware.WorkspaceContext{
				WorkspaceID: wsID,
				UserID:      userID,
				Role:        middleware.RoleOwner,
			})
			reqCtx = db.WithWorkspaceID(reqCtx, wsID)
			next.ServeHTTP(w, req.WithContext(reqCtx))
		})
	})
	RegisterRoutes(rFail, appPool, &recordingReplay{id: "offline-replay-fail"}, failingEvaluator{})

	bodyFail := `{"account_id":"` + checkingID.String() + `","total_amount":"-10.00","merchant_raw":"Late Night Diner","category_id":"` + catID.String() + `","transaction_date":"` + now.Format(time.RFC3339) + `"}`
	reqFail := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", strings.NewReader(bodyFail))
	recFail := httptest.NewRecorder()
	rFail.ServeHTTP(recFail, reqFail)
	if recFail.Code != http.StatusCreated {
		t.Fatalf("POST with failing evaluator status = %d body=%s, want 201", recFail.Code, recFail.Body.String())
	}
}
