package notifications

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getAdminDatabaseURL() string {
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
}

func getTestDatabaseURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://budget_app_user:budgetpass@localhost:5432/budgetdb?sslmode=disable"
}

func openTestPools(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, getAdminDatabaseURL())
	if err != nil {
		t.Fatalf("failed to connect to admin db: %v", err)
	}
	appPool, err := pgxpool.New(ctx, getTestDatabaseURL())
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect to app db: %v", err)
	}
	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})
	return adminPool, appPool
}

func generateTestClientKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate P256 client key: %v", err)
	}
	authBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, authBytes); err != nil {
		t.Fatalf("failed to generate auth secret: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(authBytes)
}

func generateTestVAPIDConfig(t *testing.T, subject string) VAPIDConfig {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("failed to generate VAPID keys: %v", err)
	}
	return VAPIDConfig{
		PublicKey:  pub,
		PrivateKey: priv,
		Subject:    subject,
	}
}

func TestVAPIDConfigValidation(t *testing.T) {
	t.Run("all empty is cleanly disabled", func(t *testing.T) {
		t.Setenv("VAPID_PUBLIC_KEY", "")
		t.Setenv("VAPID_PRIVATE_KEY", "")
		t.Setenv("VAPID_SUBJECT", "")

		cfg, err := LoadVAPIDConfigFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Enabled() {
			t.Fatal("expected disabled config")
		}
	})

	t.Run("mailto and https subjects are valid", func(t *testing.T) {
		for _, subj := range []string{"mailto:alerts@example.com", "https://budget.example.com/contact"} {
			t.Setenv("VAPID_PUBLIC_KEY", "pub")
			t.Setenv("VAPID_PRIVATE_KEY", "priv")
			t.Setenv("VAPID_SUBJECT", subj)

			cfg, err := LoadVAPIDConfigFromEnv()
			if err != nil {
				t.Fatalf("subject %q failed validation: %v", subj, err)
			}
			if !cfg.Enabled() {
				t.Fatalf("subject %q expected enabled", subj)
			}
		}

		cfg := VAPIDConfig{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:alerts@example.com"}
		if got := cfg.webpushSubscriber(); got != "alerts@example.com" {
			t.Fatalf("webpushSubscriber() = %q, want %q", got, "alerts@example.com")
		}
		cfgHTTPS := VAPIDConfig{PublicKey: "pub", PrivateKey: "priv", Subject: "https://budget.example.com"}
		if got := cfgHTTPS.webpushSubscriber(); got != "https://budget.example.com" {
			t.Fatalf("webpushSubscriber() = %q, want %q", got, "https://budget.example.com")
		}
	})

	t.Run("partial config returns error and disabled config", func(t *testing.T) {
		t.Setenv("VAPID_PUBLIC_KEY", "pub")
		t.Setenv("VAPID_PRIVATE_KEY", "")
		t.Setenv("VAPID_SUBJECT", "mailto:alerts@example.com")

		cfg, err := LoadVAPIDConfigFromEnv()
		if !errors.Is(err, ErrPartialVAPIDConfig) {
			t.Fatalf("err = %v, want ErrPartialVAPIDConfig", err)
		}
		if cfg.Enabled() {
			t.Fatal("partial config must return disabled VAPIDConfig")
		}
	})

	t.Run("invalid subject returns error and disabled config", func(t *testing.T) {
		t.Setenv("VAPID_PUBLIC_KEY", "pub")
		t.Setenv("VAPID_PRIVATE_KEY", "priv")
		t.Setenv("VAPID_SUBJECT", "ops@example.com")

		cfg, err := LoadVAPIDConfigFromEnv()
		if !errors.Is(err, ErrInvalidVAPIDSubject) {
			t.Fatalf("err = %v, want ErrInvalidVAPIDSubject", err)
		}
		if cfg.Enabled() {
			t.Fatal("invalid subject must return disabled VAPIDConfig")
		}
	})
}

func TestWebPushServiceSendAnd410Cleanup(t *testing.T) {
	adminPool, appPool := openTestPools(t)
	ctx := context.Background()

	userA := uuid.New()
	userB := uuid.New()
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM push_subscriptions WHERE user_id IN ($1, $2)", userA, userB)
	})

	type capturedPush struct {
		Path            string
		Method          string
		Authorization   string
		ContentEncoding string
		Body            []byte
	}

	var mu sync.Mutex
	var captured []capturedPush

	pushSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		captured = append(captured, capturedPush{
			Path:            r.URL.Path,
			Method:          r.Method,
			Authorization:   r.Header.Get("Authorization"),
			ContentEncoding: r.Header.Get("Content-Encoding"),
			Body:            body,
		})
		mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer pushSrv.Close()

	cfg := generateTestVAPIDConfig(t, "mailto:alerts@example.com")
	svc := NewWebPushService(appPool, cfg, pushSrv.Client())

	p256dh1, auth1 := generateTestClientKeys(t)
	p256dh2, auth2 := generateTestClientKeys(t)

	// Disabled service behavior
	disabledSvc := NewWebPushService(appPool, VAPIDConfig{}, pushSrv.Client())
	if err := disabledSvc.Subscribe(ctx, userA, PushSubscriptionDTO{
		Endpoint: pushSrv.URL + "/disabled",
		P256dh:   p256dh1,
		Auth:     auth1,
	}); !errors.Is(err, ErrPushDisabled) {
		t.Fatalf("Subscribe on disabled service = %v, want ErrPushDisabled", err)
	}
	if err := disabledSvc.SendNotification(ctx, userA, NotificationPayload{Title: "Ignored"}); err != nil {
		t.Fatalf("SendNotification on disabled service = %v, want nil", err)
	}

	// Subscribe device 1 (active) and device 2 (will return 410)
	epActive := pushSrv.URL + "/active"
	epGone := pushSrv.URL + "/gone"
	if err := svc.Subscribe(ctx, userA, PushSubscriptionDTO{
		Endpoint: epActive,
		P256dh:   p256dh1,
		Auth:     auth1,
	}); err != nil {
		t.Fatalf("Subscribe active failed: %v", err)
	}

	// Re-subscribing same endpoint updates keys cleanly (upsert)
	p256dh1Updated, auth1Updated := generateTestClientKeys(t)
	if err := svc.Subscribe(ctx, userA, PushSubscriptionDTO{
		Endpoint: epActive,
		P256dh:   p256dh1Updated,
		Auth:     auth1Updated,
	}); err != nil {
		t.Fatalf("re-Subscribe active failed: %v", err)
	}

	if err := svc.Subscribe(ctx, userA, PushSubscriptionDTO{
		Endpoint: epGone,
		P256dh:   p256dh2,
		Auth:     auth2,
	}); err != nil {
		t.Fatalf("Subscribe gone failed: %v", err)
	}

	// Verify RLS isolation: userB sees zero subscriptions of userA
	var userBCount int
	if err := db.WithWorkspaceTx(ctx, appPool, userB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT COUNT(*) FROM push_subscriptions").Scan(&userBCount)
	}); err != nil {
		t.Fatalf("RLS check query failed: %v", err)
	}
	if userBCount != 0 {
		t.Fatalf("userB saw %d push_subscriptions belonging to userA; expected 0", userBCount)
	}

	payload := NotificationPayload{
		Title: "Spending Alert",
		Body:  "Dining is at 85% of your monthly budget.",
		URL:   "/budget",
	}
	if err := svc.SendNotification(ctx, userA, payload); err != nil {
		t.Fatalf("SendNotification failed: %v", err)
	}

	mu.Lock()
	got := append([]capturedPush(nil), captured...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("expected 2 push HTTP deliveries, got %d", len(got))
	}
	for _, req := range got {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if !strings.HasPrefix(req.Authorization, "vapid t=") {
			t.Fatalf("expected RFC 8292 VAPID Authorization header, got %q", req.Authorization)
		}
		if req.ContentEncoding != "aes128gcm" {
			t.Fatalf("expected Content-Encoding aes128gcm, got %q", req.ContentEncoding)
		}
		if bytes.Contains(req.Body, []byte("Spending Alert")) || len(req.Body) == 0 {
			t.Fatalf("expected encrypted non-empty push payload, got len=%d", len(req.Body))
		}
	}

	// Verify 410 subscription was deleted while active subscription remains with updated keys
	var remaining []PushSubscriptionDTO
	if err := db.WithWorkspaceTx(ctx, appPool, userA, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = $1", userA)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var dto PushSubscriptionDTO
			if err := rows.Scan(&dto.Endpoint, &dto.P256dh, &dto.Auth); err != nil {
				return err
			}
			remaining = append(remaining, dto)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("failed to query remaining subscriptions: %v", err)
	}

	if len(remaining) != 1 || remaining[0].Endpoint != epActive {
		t.Fatalf("expected only %q remaining after 410 cleanup, got %+v", epActive, remaining)
	}
	if remaining[0].P256dh != p256dh1Updated || remaining[0].Auth != auth1Updated {
		t.Fatalf("expected upserted keys on active subscription, got %+v", remaining[0])
	}

	// Unsubscribe removes active subscription and is idempotent
	if err := svc.Unsubscribe(ctx, userA, epActive); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}
	if err := svc.Unsubscribe(ctx, userA, epActive); err != nil {
		t.Fatalf("idempotent Unsubscribe failed: %v", err)
	}
	var afterUnsub int
	if err := db.WithWorkspaceTx(ctx, appPool, userA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $1", userA).Scan(&afterUnsub)
	}); err != nil {
		t.Fatalf("count after unsubscribe failed: %v", err)
	}
	if afterUnsub != 0 {
		t.Fatalf("expected 0 subscriptions after unsubscribe, got %d", afterUnsub)
	}
}

type stubPushService struct {
	enabled         bool
	publicKey       string
	lastSubscribe   *PushSubscriptionDTO
	lastUnsubscribe string
	subscribeErr    error
	unsubscribeErr  error
}

func (s *stubPushService) Enabled() bool     { return s.enabled }
func (s *stubPushService) PublicKey() string { return s.publicKey }
func (s *stubPushService) Subscribe(_ context.Context, _ uuid.UUID, sub PushSubscriptionDTO) error {
	if s.subscribeErr != nil {
		return s.subscribeErr
	}
	s.lastSubscribe = &sub
	return nil
}
func (s *stubPushService) Unsubscribe(_ context.Context, _ uuid.UUID, endpoint string) error {
	if s.unsubscribeErr != nil {
		return s.unsubscribeErr
	}
	s.lastUnsubscribe = endpoint
	return nil
}
func (s *stubPushService) SendNotification(context.Context, uuid.UUID, NotificationPayload) error {
	return nil
}

func testRouterWithUser(svc PushService, userID *uuid.UUID) chi.Router {
	r := chi.NewRouter()
	if userID != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := context.WithValue(req.Context(), middleware.UserIDKey, *userID)
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
	}
	RegisterRoutes(r, svc)
	return r
}

func TestNotificationHandlers(t *testing.T) {
	uid := uuid.New()

	t.Run("unauthorized requests return 401", func(t *testing.T) {
		r := testRouterWithUser(&stubPushService{enabled: true, publicKey: "pub"}, nil)
		for _, tc := range []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/v1/notifications/config"},
			{http.MethodPost, "/api/v1/notifications/subscribe"},
			{http.MethodDelete, "/api/v1/notifications/subscribe?endpoint=https://example.com"},
		} {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s status = %d, want 401", tc.method, tc.path, rec.Code)
			}
		}
	})

	t.Run("config reports disabled and enabled states", func(t *testing.T) {
		rDisabled := testRouterWithUser(&stubPushService{enabled: false}, &uid)
		rec := httptest.NewRecorder()
		rDisabled.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/config", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body struct {
			Enabled        bool   `json:"enabled"`
			VAPIDPublicKey string `json:"vapid_public_key"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if body.Enabled || body.VAPIDPublicKey != "" {
			t.Fatalf("unexpected disabled config body: %+v", body)
		}

		rEnabled := testRouterWithUser(&stubPushService{enabled: true, publicKey: "vapid-pub-123"}, &uid)
		rec = httptest.NewRecorder()
		rEnabled.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notifications/config", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if !body.Enabled || body.VAPIDPublicKey != "vapid-pub-123" {
			t.Fatalf("unexpected enabled config body: %+v", body)
		}
	})

	t.Run("subscribe returns 503 when disabled, 400 on invalid, 201 on flat or nested payload", func(t *testing.T) {
		rDisabled := testRouterWithUser(&stubPushService{enabled: false}, &uid)
		rec := httptest.NewRecorder()
		rDisabled.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/subscribe", strings.NewReader(`{"endpoint":"https://push.example/1","p256dh":"a","auth":"b"}`)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("disabled subscribe status = %d, want 503", rec.Code)
		}

		svc := &stubPushService{enabled: true, publicKey: "pub"}
		r := testRouterWithUser(svc, &uid)

		// Flat DTO payload
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/subscribe", strings.NewReader(`{"endpoint":"https://push.example/flat","p256dh":"p1","auth":"a1"}`)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("flat subscribe status = %d, want 201", rec.Code)
		}
		if svc.lastSubscribe == nil || svc.lastSubscribe.Endpoint != "https://push.example/flat" || svc.lastSubscribe.P256dh != "p1" || svc.lastSubscribe.Auth != "a1" {
			t.Fatalf("unexpected flat subscription: %+v", svc.lastSubscribe)
		}

		// Browser PushSubscription.toJSON() nested keys payload
		svc.lastSubscribe = nil
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/subscribe", strings.NewReader(`{"endpoint":"https://push.example/nested","keys":{"p256dh":"p2","auth":"a2"}}`)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("nested subscribe status = %d, want 201", rec.Code)
		}
		if svc.lastSubscribe == nil || svc.lastSubscribe.Endpoint != "https://push.example/nested" || svc.lastSubscribe.P256dh != "p2" || svc.lastSubscribe.Auth != "a2" {
			t.Fatalf("unexpected nested subscription: %+v", svc.lastSubscribe)
		}

		// Invalid subscription error mapping
		rInvalid := testRouterWithUser(&stubPushService{enabled: true, subscribeErr: ErrInvalidSubscription}, &uid)
		rec = httptest.NewRecorder()
		rInvalid.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notifications/subscribe", strings.NewReader(`{"endpoint":"bad"}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid subscribe status = %d, want 400", rec.Code)
		}
	})

	t.Run("unsubscribe accepts JSON body or query param and requires endpoint", func(t *testing.T) {
		svc := &stubPushService{enabled: false}
		r := testRouterWithUser(svc, &uid)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/subscribe", strings.NewReader(`{"endpoint":"https://push.example/body"}`)))
		if rec.Code != http.StatusNoContent || svc.lastUnsubscribe != "https://push.example/body" {
			t.Fatalf("body unsubscribe got status=%d endpoint=%q", rec.Code, svc.lastUnsubscribe)
		}

		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/subscribe?endpoint=https://push.example/query", nil))
		if rec.Code != http.StatusNoContent || svc.lastUnsubscribe != "https://push.example/query" {
			t.Fatalf("query unsubscribe got status=%d endpoint=%q", rec.Code, svc.lastUnsubscribe)
		}

		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/subscribe", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("missing endpoint status = %d, want 400", rec.Code)
		}
	})
}
