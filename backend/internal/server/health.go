package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
)

type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}

func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// HealthHandler is the readiness probe. Temporal is a required dependency, so an
// unreachable server or unresolvable namespace makes the pod unready and pulls it out of
// the Service rotation: an app that answers from the browser while every bank sync is
// silently failing is worse than one that is visibly down.
func HealthHandler(pool *pgxpool.Pool, temporalClient client.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		checks := make(map[string]string)
		status := "ok"
		statusCode := http.StatusOK

		if pool == nil {
			checks["postgres"] = "not_configured"
		} else if err := pool.Ping(ctx); err != nil {
			checks["postgres"] = "unhealthy"
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		} else {
			checks["postgres"] = "healthy"
		}

		switch {
		case temporalClient == nil:
			checks["temporal"] = "unconfigured"
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		default:
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := temporalinternal.HealthCheck(checkCtx, temporalClient)
			cancel()
			if err != nil {
				checks["temporal"] = "unhealthy"
				status = "degraded"
				statusCode = http.StatusServiceUnavailable
			} else {
				checks["temporal"] = "healthy"
			}
		}

		resp := HealthResponse{
			Status:    status,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Checks:    checks,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
