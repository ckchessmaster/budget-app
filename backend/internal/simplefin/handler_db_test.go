package simplefin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type recordingClaim struct {
	calls  int
	scope  temporalinternal.SyncScope
	runID  string
	err    error
	called bool
}

func (r *recordingClaim) Start(_ context.Context, scope temporalinternal.SyncScope) (string, error) {
	r.calls++
	r.scope = scope
	r.called = true
	if r.err != nil {
		return "", r.err
	}
	return r.runID, nil
}

func (r *recordingClaim) Discover(_ context.Context, scope temporalinternal.SyncScope) (string, error) {
	return r.Start(context.Background(), scope)
}

func openHandlerTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("skipping handler test (pool error): %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping handler test (postgres unreachable): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newClaimRequestContext(t *testing.T, pool *pgxpool.Pool) (context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	wsID, userID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Claim Handler WS')", wsID); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM simplefin_connections WHERE workspace_id = $1", wsID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})
	return context.WithValue(ctx, middleware.UserIDKey, userID), wsID, userID
}

func claimRequest(ctx context.Context, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/bank/simplefin/claim", strings.NewReader(body))
	return req.WithContext(ctx)
}

// TestHandlerHoldsNoBankClient pins the structural half of the ticket: the claim request
// path must not dial a bank at all. A field check catches a reintroduced client even in
// code paths a request test would never reach.
func TestHandlerHoldsNoBankClient(t *testing.T) {
	typ := reflect.TypeOf(Handler{})
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.AssignableTo(clientType) || field.Type == clientType {
			t.Fatalf("Handler field %s is a simplefin.Client; the request path must not make outbound calls", field.Name)
		}
	}
}

func TestClaimTokenStagesTheTokenAndStartsAWorkflow(t *testing.T) {
	pool := openHandlerTestDB(t)
	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build vault: %v", err)
	}

	claim := &recordingClaim{runID: "simplefin-claim-run-1"}
	h := NewHandler(pool, vault, claim)

	reqCtx, wsID, userID := newClaimRequestContext(t, pool)
	const setupToken = "a-real-looking-base64-setup-token"

	rec := httptest.NewRecorder()
	h.ClaimToken(rec, claimRequest(reqCtx, `{"setup_token":"`+setupToken+`"}`))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["workflow_id"] != "simplefin-claim-run-1" || body["status"] != "accepted" {
		t.Fatalf("body = %v, want the workflow id and accepted", body)
	}
	if !claim.called || claim.scope.WorkspaceID != wsID || claim.scope.UserID != userID {
		t.Fatalf("workflow starter got %v", claim.scope)
	}

	var pending, access string
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(pending_claim_token, ''), COALESCE(encrypted_access_url, '')
		FROM simplefin_connections WHERE workspace_id = $1
	`, wsID).Scan(&pending, &access); err != nil {
		t.Fatalf("failed to read staged connection: %v", err)
	}
	if pending == "" {
		t.Fatalf("the setup token was never staged")
	}
	if pending == setupToken {
		t.Fatalf("the setup token was stored in plaintext")
	}
	opened, err := vault.Decrypt(pending)
	if err != nil || opened != setupToken {
		t.Fatalf("staged token did not round-trip through the vault: %v", err)
	}
	// Staging must not disturb an existing connection: a failed claim should cost the
	// user nothing.
	if access != "" {
		t.Fatalf("staging wrote an access URL; that is the worker's job")
	}
}

func TestClaimTokenRejectsMissingTokenBeforeTouchingAnything(t *testing.T) {
	pool := openHandlerTestDB(t)
	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build vault: %v", err)
	}
	claim := &recordingClaim{}
	h := NewHandler(pool, vault, claim)

	reqCtx, _, _ := newClaimRequestContext(t, pool)
	rec := httptest.NewRecorder()
	h.ClaimToken(rec, claimRequest(reqCtx, `{}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if claim.called {
		t.Fatalf("an empty request must not start a workflow")
	}
}

func TestClaimTokenReportsUnavailableTemporal(t *testing.T) {
	pool := openHandlerTestDB(t)
	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build vault: %v", err)
	}
	h := NewHandler(pool, vault, nil)

	reqCtx, _, _ := newClaimRequestContext(t, pool)
	rec := httptest.NewRecorder()
	h.ClaimToken(rec, claimRequest(reqCtx, `{"setup_token":"abc"}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
