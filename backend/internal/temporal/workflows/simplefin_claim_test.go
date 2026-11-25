package workflows

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// fakeBank is a stand-in SimpleFIN provider. The claim endpoint is single-use, so the
// number of POSTs it sees is the assertion that matters most in this file: a retry must
// not re-dial it once a claim has succeeded.
type fakeBank struct {
	server        *httptest.Server
	claimCalls    atomic.Int32
	fetchCalls    atomic.Int32
	firstClaim    int // status to return on the first claim only; 0 succeeds immediately
	rateLimitWait string
}

func newFakeBank(t *testing.T, firstClaimStatus int, retryAfter string) *fakeBank {
	t.Helper()
	b := &fakeBank{firstClaim: firstClaimStatus, rateLimitWait: retryAfter}

	mux := http.NewServeMux()
	mux.HandleFunc("/claim/", func(w http.ResponseWriter, r *http.Request) {
		attempt := b.claimCalls.Add(1)
		if attempt == 1 && b.firstClaim != 0 {
			if b.rateLimitWait != "" {
				w.Header().Set("Retry-After", b.rateLimitWait)
			}
			w.WriteHeader(b.firstClaim)
			_, _ = w.Write([]byte("rejected"))
			return
		}
		// A SimpleFIN claim answers with the bare access URL, credentials included.
		_, _ = fmt.Fprintf(w, "%s/access/acct-1", b.authority())
	})
	mux.HandleFunc("/access/", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "id123" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b.fetchCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":[{"id":"acct-1","name":"Everyday Checking","currency":"USD","balance":"102.50"}]}`))
	})

	b.server = httptest.NewServer(mux)
	t.Cleanup(b.server.Close)
	return b
}

// authority is the host:port of the fake, without a scheme, so an access URL can carry
// userinfo and still dial the test server.
func (b *fakeBank) authority() string {
	return "http://id123:secret@" + strings.TrimPrefix(b.server.URL, "http://")
}

func (b *fakeBank) setupToken() string {
	claimURL := b.server.URL + "/claim/token-abc"
	return base64.StdEncoding.EncodeToString([]byte(claimURL))
}

func openClaimTestDB(t *testing.T) (adminPool, appPool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	appURL := os.Getenv("TEST_DATABASE_URL")
	if appURL == "" {
		appURL = adminURL
	}

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("skipping claim simulation (admin pool error): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping claim simulation (postgres unreachable): %v", err)
	}
	appPool, err = pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping claim simulation (app pool error): %v", err)
	}
	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})
	return adminPool, appPool
}

func newClaimVault(t *testing.T) crypto.Vault {
	t.Helper()
	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build vault: %v", err)
	}
	return vault
}

func TestSimpleFINClaimWorkflowHonoursRetryAfterThenStoresConnection(t *testing.T) {
	adminPool, appPool := openClaimTestDB(t)
	ctx := context.Background()

	wsID, userID := uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Claim Retry WS')", wsID); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM simplefin_connections WHERE workspace_id = $1", wsID)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	bank := newFakeBank(t, http.StatusTooManyRequests, "1")

	vault := newClaimVault(t)
	sealed, err := vault.Encrypt(bank.setupToken())
	if err != nil {
		t.Fatalf("failed to seal setup token: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, pending_claim_token)
		VALUES ($1, $2, $2)
	`, wsID, sealed); err != nil {
		t.Fatalf("failed to stage pending claim: %v", err)
	}

	// The bank says "come back in one second" once. The workflow must wait that long and
	// try again rather than surfacing a 429 to the user, which is the behaviour the client
	// has modelled since long before anything acted on it.
	claimActs := &activities.ClaimActivities{
		Pool:   appPool,
		Client: simplefin.NewClient(bank.server.Client()),
		Vault:  vault,
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(claimActs)
	env.ExecuteWorkflow(SimpleFINClaimWorkflow, SimpleFINClaimInput{
		SyncScope: temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatalf("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if got := bank.claimCalls.Load(); got != 2 {
		t.Fatalf("claim endpoint called %d times, want 2 (one rate limit, one success)", got)
	}

	var result SimpleFINClaimResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if result.AccountsDiscovered != 1 || len(result.Accounts) != 1 {
		t.Fatalf("discovered %d accounts, want 1", result.AccountsDiscovered)
	}
	if result.Accounts[0].Name != "Everyday Checking" {
		t.Fatalf("account name = %q", result.Accounts[0].Name)
	}
	if result.ConnectionID != wsID.String() {
		t.Fatalf("connection id = %q, want workspace id %q", result.ConnectionID, wsID)
	}

	var accessURL, pending string
	if err := adminPool.QueryRow(ctx, `
		SELECT COALESCE(encrypted_access_url, ''), COALESCE(pending_claim_token, '')
		FROM simplefin_connections WHERE workspace_id = $1
	`, wsID).Scan(&accessURL, &pending); err != nil {
		t.Fatalf("failed to read connection: %v", err)
	}
	if accessURL == "" {
		t.Fatalf("claim did not store an access URL")
	}
	if pending != "" {
		t.Fatalf("a completed claim left a staged token behind")
	}
	if accessURL == sealed {
		t.Fatalf("stored access URL was not sealed")
	}
}

func TestSimpleFINClaimWorkflowFailsFastOnRejectedToken(t *testing.T) {
	adminPool, appPool := openClaimTestDB(t)
	ctx := context.Background()

	wsID, userID := uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Claim Rejected WS')", wsID); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM simplefin_connections WHERE workspace_id = $1", wsID)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	bank := newFakeBank(t, http.StatusBadRequest, "")

	vault := newClaimVault(t)
	sealed, err := vault.Encrypt(bank.setupToken())
	if err != nil {
		t.Fatalf("failed to seal setup token: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, pending_claim_token)
		VALUES ($1, $2, $2)
	`, wsID, sealed); err != nil {
		t.Fatalf("failed to stage pending claim: %v", err)
	}

	claimActs := &activities.ClaimActivities{
		Pool:   appPool,
		Client: simplefin.NewClient(bank.server.Client()),
		Vault:  vault,
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(claimActs)
	env.ExecuteWorkflow(SimpleFINClaimWorkflow, SimpleFINClaimInput{
		SyncScope: temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatalf("workflow did not complete")
	}
	wfErr := env.GetWorkflowError()
	if wfErr == nil {
		t.Fatalf("expected the workflow to fail on a rejected token")
	}
	// A 400 means the token is spent or malformed. Retrying cannot help and each attempt
	// is another request to a bank that has already said no.
	if got := bank.claimCalls.Load(); got != 1 {
		t.Fatalf("claim endpoint called %d times, want exactly 1 (no retries)", got)
	}

	// The terminal classification has to survive to the caller: a user staring at a
	// "retrying" spinner on a spent token is the exact behaviour this ticket exists to end.
	var applicationErr *temporal.ApplicationError
	if !errors.As(wfErr, &applicationErr) {
		t.Fatalf("workflow error = %T, want *temporal.ApplicationError", wfErr)
	}
	if applicationErr.Type() != "SimpleFINTokenRejected" {
		t.Fatalf("error type = %q, want SimpleFINTokenRejected", applicationErr.Type())
	}
	if !applicationErr.NonRetryable() {
		t.Fatalf("a rejected token must be classified non-retryable")
	}
	if bank.fetchCalls.Load() != 0 {
		t.Fatalf("the workflow fetched accounts after a failed claim")
	}
}

func TestClaimConnectionActivityDoesNotReclaimACompletedClaim(t *testing.T) {
	adminPool, appPool := openClaimTestDB(t)
	ctx := context.Background()

	wsID, userID := uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name) VALUES ($1, 'Claim Idempotent WS')", wsID); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM simplefin_connections WHERE workspace_id = $1", wsID)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	bank := newFakeBank(t, 0, "")

	vault := newClaimVault(t)
	sealed, err := vault.Encrypt(bank.setupToken())
	if err != nil {
		t.Fatalf("failed to seal: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, pending_claim_token)
		VALUES ($1, $2, $2)
	`, wsID, sealed); err != nil {
		t.Fatalf("failed to stage: %v", err)
	}

	claimActs := &activities.ClaimActivities{
		Pool:   appPool,
		Client: simplefin.NewClient(bank.server.Client()),
		Vault:  vault,
	}
	scope := temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}

	first, err := claimActs.ClaimConnectionActivity(ctx, scope)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if first.Reused {
		t.Fatalf("the first claim should have claimed, not reused")
	}

	// A late duplicate must report success without touching the bank again; the token is
	// single-use, so a second POST would fail a workflow that already succeeded.
	second, err := claimActs.ClaimConnectionActivity(ctx, scope)
	if err != nil {
		t.Fatalf("duplicate claim failed: %v", err)
	}
	if !second.Reused {
		t.Fatalf("duplicate claim should have reused the stored connection")
	}
	if got := bank.claimCalls.Load(); got != 1 {
		t.Fatalf("claim endpoint called %d times, want 1", got)
	}
}

func TestSimpleFINClaimWorkflowInputCarriesNoCredential(t *testing.T) {
	// Workflow inputs are recorded verbatim in Temporal history, which operators can read
	// and which is retained for the whole retention window. If a token field ever reappears
	// here, this fails on the change instead of surfacing later as a credential in history.
	names := map[string]bool{}
	typ := reflect.TypeOf(SimpleFINClaimInput{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous {
			inner := f.Type
			for j := 0; j < inner.NumField(); j++ {
				names[jsonName(inner.Field(j))] = true
			}
			continue
		}
		names[jsonName(f)] = true
	}

	want := map[string]bool{"workspace_id": true, "user_id": true}
	if len(names) != len(want) {
		t.Fatalf("SimpleFINClaimInput fields = %v, want exactly %v", names, want)
	}
	for name := range want {
		if !names[name] {
			t.Fatalf("SimpleFINClaimInput is missing %q (got %v)", name, names)
		}
	}
}

func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	return strings.Split(tag, ",")[0]
}
