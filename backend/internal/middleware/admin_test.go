package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adminClaimsMockVerifier struct {
	claims map[string]middleware.TokenClaims
}

func (m *adminClaimsMockVerifier) Verify(ctx context.Context, rawToken string) (string, error) {
	c, err := m.VerifyClaims(ctx, rawToken)
	return c.Subject, err
}

func (m *adminClaimsMockVerifier) VerifyClaims(_ context.Context, rawToken string) (middleware.TokenClaims, error) {
	c, ok := m.claims[rawToken]
	if !ok {
		return middleware.TokenClaims{}, middleware.ErrInvalidToken
	}
	return c, nil
}

func TestRequirePlatformAdmin(t *testing.T) {
	adminID := uuid.New()
	memberID := uuid.New()

	verifier := &adminClaimsMockVerifier{
		claims: map[string]middleware.TokenClaims{
			"admin-token": {
				Subject: adminID.String(),
				Email:   "chris@example.com",
				Name:    "Chris",
				Groups:  []string{"users", "budget-admins"},
			},
			"member-token": {
				Subject: memberID.String(),
				Email:   "mike@example.com",
				Name:    "Mike",
				Groups:  []string{"users"},
			},
			"dex-admin-token": {
				Subject: adminID.String(),
				Email:   "admin@dev.local",
				Name:    "Local Admin",
				Groups:  nil,
			},
		},
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	cfg := middleware.PlatformAdminConfig{
		AdminGroup:  "budget-admins",
		AdminEmails: []string{"admin@dev.local"},
	}

	chain := middleware.AuthMiddleware(verifier)(
		middleware.RequirePlatformAdmin(nil, cfg)(okHandler),
	)

	t.Run("valid JWT with budget-admins passes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid JWT without budget-admins gets 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req.Header.Set("Authorization", "Bearer member-token")
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("local dex admin email fallback passes when configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req.Header.Set("Authorization", "Bearer dex-admin-token")
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("disabled user in platform_users gets 403 account_disabled", func(t *testing.T) {
		dbURL := os.Getenv("TEST_DATABASE_URL")
		if dbURL == "" {
			dbURL = os.Getenv("DATABASE_URL")
		}
		if dbURL == "" {
			t.Skip("DATABASE_URL / TEST_DATABASE_URL not set; skipping DB disabled-user middleware check")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool, err := pgxpool.New(ctx, dbURL)
		if err != nil {
			t.Fatalf("connect db: %v", err)
		}
		defer pool.Close()

		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM platform_users WHERE user_id IN ($1, $2)", adminID, memberID)
		}()

		dbChain := middleware.AuthMiddleware(verifier)(
			middleware.EnsureActivePlatformUser(pool)(
				middleware.RequirePlatformAdmin(pool, cfg)(okHandler),
			),
		)

		// First request auto-upserts adminID as active
		req1 := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req1.Header.Set("Authorization", "Bearer admin-token")
		rec1 := httptest.NewRecorder()
		dbChain.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("initial request status = %d, want 200: %s", rec1.Code, rec1.Body.String())
		}

		// Disable user in platform_users
		if _, err := pool.Exec(ctx, "UPDATE platform_users SET is_active = FALSE WHERE user_id = $1", adminID); err != nil {
			t.Fatalf("disable user: %v", err)
		}

		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		req2.Header.Set("Authorization", "Bearer admin-token")
		rec2 := httptest.NewRecorder()
		dbChain.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusForbidden {
			t.Fatalf("disabled user status = %d, want 403: %s", rec2.Code, rec2.Body.String())
		}
		var body map[string]string
		_ = json.Unmarshal(rec2.Body.Bytes(), &body)
		if body["error"] != "account_disabled" {
			t.Fatalf("error = %q, want account_disabled", body["error"])
		}
	})
}
