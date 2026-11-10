package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
)

type mockVerifier struct {
	verifyFunc func(ctx context.Context, rawToken string) (string, error)
}

func (m *mockVerifier) Verify(ctx context.Context, rawToken string) (string, error) {
	return m.verifyFunc(ctx, rawToken)
}

func TestAuthMiddleware(t *testing.T) {
	testUserID := uuid.New()

	verifier := &mockVerifier{
		verifyFunc: func(ctx context.Context, rawToken string) (string, error) {
			switch rawToken {
			case "valid-token":
				return testUserID.String(), nil
			case "expired-token":
				return "", errors.New("token expired")
			case "invalid-uuid-token":
				return "not-a-uuid", nil
			default:
				return "", errors.New("invalid signature")
			}
		},
	}

	handler := middleware.AuthMiddleware(verifier)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, err := middleware.GetUserID(r.Context())
		if err != nil {
			t.Errorf("expected to extract userID from context, got error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if uid != testUserID {
			t.Errorf("expected userID %s, got %s", testUserID, uid)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))

	// Test Case 1: Missing auth header returns 401
	t.Run("Missing auth header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if body["error"] != "missing_token" {
			t.Fatalf("expected error missing_token, got %s", body["error"])
		}
	})

	// Test Case 2: Expired or invalid token returns 401
	t.Run("Expired token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer expired-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if body["error"] != "invalid_token" {
			t.Fatalf("expected error invalid_token, got %s", body["error"])
		}
	})

	// Test Case 3: Valid token returns 200 with extracted UUID in context
	t.Run("Valid token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
