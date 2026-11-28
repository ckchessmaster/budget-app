package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/server"
)

// TestSharedWorkflowStatusRouteIsReachable guards the offline queue's status path.
//
// The transaction routes are mounted as a chi subrouter at /api/v1/transactions, which also
// claims /api/v1/transactions/* — so a status route registered on the parent can be shadowed
// by it. A shadowed route fails where it matters most: the browser has already handed the
// batch over, and a 404 on the poll looks like the work was lost.
//
// An unauthenticated request proves which one happened. A registered route reaches the auth
// middleware and answers 401; a shadowed one is answered by the subrouter with 404.
func TestSharedWorkflowStatusRouteIsReachable(t *testing.T) {
	verifier := &stubVerifier{err: errors.New("bad signature")}
	cfg := server.Config{
		Port:                "8080",
		OIDCIssuerURL:       "https://auth.example.com/application/o/budget-app/",
		OIDCClientID:        "budget-app-client",
		OIDCEnforceAudience: true,
	}
	router := server.NewServer(cfg, nil, verifier, nil, nil).Router()

	for _, path := range []string{
		"/api/v1/transactions/workflow-status/offline-replay-11111111-2222-3333-4444-555555555555-1",
		"/api/v1/bank/workflow-status/simplefin-claim-1-1",
		"/api/v1/bank/sync-status/bank-sync-1-1",
		"/api/v1/auth/me",
		"/api/v1/admin/users",
		"/api/v1/admin/health",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Fatalf("GET %s = 404: the route is shadowed by a subrouter mount", path)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s = %d, want 401 from the auth middleware", path, rec.Code)
			}
		})
	}
}
