package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type ContextKey string

const UserIDKey ContextKey = "current_user_id"

var (
	ErrMissingToken = errors.New("missing_token")
	ErrInvalidToken = errors.New("invalid_token")
	ErrUserNotFound = errors.New("user_id not found in context")
)

type OIDCVerifier interface {
	Verify(ctx context.Context, rawToken string) (string, error)
}

func AuthMiddleware(verifier OIDCVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				writeErrorJSON(w, http.StatusUnauthorized, "missing_token")
				return
			}

			rawToken := strings.TrimSpace(authHeader[7:])
			if rawToken == "" {
				writeErrorJSON(w, http.StatusUnauthorized, "missing_token")
				return
			}

			subject, err := verifier.Verify(r.Context(), rawToken)
			if err != nil {
				writeErrorJSON(w, http.StatusUnauthorized, "invalid_token")
				return
			}

			userID, err := uuid.Parse(subject)
			if err != nil {
				writeErrorJSON(w, http.StatusUnauthorized, "invalid_token")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserID(ctx context.Context) (uuid.UUID, error) {
	val := ctx.Value(UserIDKey)
	if val == nil {
		return uuid.Nil, ErrUserNotFound
	}
	userID, ok := val.(uuid.UUID)
	if !ok {
		return uuid.Nil, ErrUserNotFound
	}
	return userID, nil
}

func writeErrorJSON(w http.ResponseWriter, statusCode int, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": errMsg,
	})
}
