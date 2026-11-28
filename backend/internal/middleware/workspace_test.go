package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openMiddlewareTestPools(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	appURL := os.Getenv("TEST_DATABASE_URL")
	if appURL == "" {
		appURL = "postgres://budget_app_user:budgetpass@localhost:5432/budgetdb?sslmode=disable"
	}

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("skipping workspace middleware DB test (admin connect): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping workspace middleware DB test (postgres unreachable): %v", err)
	}
	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping workspace middleware DB test (app connect): %v", err)
	}
	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})
	return adminPool, appPool
}

type claimsMockVerifier struct {
	users map[string]middleware.TokenClaims
}

func (v *claimsMockVerifier) Verify(_ context.Context, rawToken string) (string, error) {
	c, ok := v.users[rawToken]
	if !ok {
		return "", middleware.ErrInvalidToken
	}
	return c.Subject, nil
}

func (v *claimsMockVerifier) VerifyClaims(_ context.Context, rawToken string) (middleware.TokenClaims, error) {
	c, ok := v.users[rawToken]
	if !ok {
		return middleware.TokenClaims{}, middleware.ErrInvalidToken
	}
	return c, nil
}

func TestRequireWorkspaceAndRequireRole(t *testing.T) {
	adminPool, appPool := openMiddlewareTestPools(t)
	ctx := context.Background()

	ownerID := uuid.New()
	memberID := uuid.New()
	readOnlyID := uuid.New()
	strangerID := uuid.New()

	wsID := uuid.New()
	otherWS := uuid.New()

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo, owner_user_id)
		VALUES ($1, 'Household WS', FALSE, $3),
		       ($2, 'Other WS', FALSE, $4)
	`, wsID, otherWS, ownerID, strangerID); err != nil {
		t.Fatalf("seed workspaces: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $3, 'owner'),
		       ($1, $4, 'member'),
		       ($1, $5, 'read_only'),
		       ($2, $6, 'owner')
	`, wsID, otherWS, ownerID, memberID, readOnlyID, strangerID); err != nil {
		t.Fatalf("seed members: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id IN ($1, $2)", wsID, otherWS)
	})

	verifier := &claimsMockVerifier{
		users: map[string]middleware.TokenClaims{
			"owner-tok":    {Subject: ownerID.String(), Email: "owner@example.com", Name: "Owner"},
			"member-tok":   {Subject: memberID.String(), Email: "member@example.com", Name: "Member"},
			"readonly-tok": {Subject: readOnlyID.String(), Email: "ro@example.com", Name: "Reader"},
			"stranger-tok": {Subject: strangerID.String(), Email: "stranger@example.com", Name: "Stranger"},
		},
	}

	r := chi.NewRouter()
	r.Use(middleware.AuthMiddleware(verifier))
	r.Use(middleware.RequireWorkspace(appPool, workspace.NewProvisioner()))

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wc, err := middleware.GetWorkspaceContext(r.Context())
		if err != nil {
			t.Fatalf("expected WorkspaceContext: %v", err)
		}
		gotWS, ok := db.WorkspaceIDFromContext(r.Context())
		if !ok || gotWS != wc.WorkspaceID {
			t.Fatalf("expected db.WorkspaceIDFromContext %s, got %s", wc.WorkspaceID, gotWS)
		}
		w.WriteHeader(http.StatusOK)
	})

	r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/api/v1/accounts", okHandler)
	r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/api/v1/transactions", okHandler)
	r.With(middleware.RequireRole(middleware.RoleOwner)).Post("/api/v1/bank/simplefin/claim", okHandler)
	r.With(middleware.RequireRole(middleware.RoleOwner)).Post("/api/v1/workspace/invitations", okHandler)
	r.With(middleware.RequireRole(middleware.RoleOwner)).Patch("/api/v1/settings/workspace", okHandler)

	doReq := func(method, path, token, wsHeader string) int {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if wsHeader != "" {
			req.Header.Set("X-Workspace-ID", wsHeader)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// 1. Supplying an X-Workspace-ID the user is not a member of returns 403 Forbidden.
	if code := doReq(http.MethodGet, "/api/v1/accounts", "member-tok", otherWS.String()); code != http.StatusForbidden {
		t.Fatalf("non-member X-Workspace-ID got %d, want 403", code)
	}
	if code := doReq(http.MethodGet, "/api/v1/accounts", "member-tok", "not-a-uuid"); code != http.StatusBadRequest {
		t.Fatalf("malformed X-Workspace-ID got %d, want 400", code)
	}

	// 2. member receives 200 OK on GET /accounts and POST /transactions, and 403 on owner-only endpoints.
	if code := doReq(http.MethodGet, "/api/v1/accounts", "member-tok", wsID.String()); code != http.StatusOK {
		t.Fatalf("member GET /accounts got %d, want 200", code)
	}
	if code := doReq(http.MethodPost, "/api/v1/transactions", "member-tok", wsID.String()); code != http.StatusOK {
		t.Fatalf("member POST /transactions got %d, want 200", code)
	}
	for _, ownerOnlyPath := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/bank/simplefin/claim"},
		{http.MethodPost, "/api/v1/workspace/invitations"},
		{http.MethodPatch, "/api/v1/settings/workspace"},
	} {
		if code := doReq(ownerOnlyPath.method, ownerOnlyPath.path, "member-tok", wsID.String()); code != http.StatusForbidden {
			t.Fatalf("member %s %s got %d, want 403", ownerOnlyPath.method, ownerOnlyPath.path, code)
		}
		if code := doReq(ownerOnlyPath.method, ownerOnlyPath.path, "owner-tok", wsID.String()); code != http.StatusOK {
			t.Fatalf("owner %s %s got %d, want 200", ownerOnlyPath.method, ownerOnlyPath.path, code)
		}
	}

	// 3. read_only receives 200 on GET and 403 on POST /transactions.
	if code := doReq(http.MethodGet, "/api/v1/accounts", "readonly-tok", wsID.String()); code != http.StatusOK {
		t.Fatalf("read_only GET /accounts got %d, want 200", code)
	}
	if code := doReq(http.MethodPost, "/api/v1/transactions", "readonly-tok", wsID.String()); code != http.StatusForbidden {
		t.Fatalf("read_only POST /transactions got %d, want 403", code)
	}

	// Verify workspace_roster was populated with email + display_name for member
	var rosterEmail, rosterName string
	if err := adminPool.QueryRow(ctx, `
		SELECT email, display_name FROM workspace_roster WHERE workspace_id = $1 AND user_id = $2
	`, wsID, memberID).Scan(&rosterEmail, &rosterName); err != nil {
		t.Fatalf("expected workspace_roster entry for member: %v", err)
	}
	if rosterEmail != "member@example.com" || rosterName != "Member" {
		t.Fatalf("roster entry = (%q, %q), want (member@example.com, Member)", rosterEmail, rosterName)
	}
}
