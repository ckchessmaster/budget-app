package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/email"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// TestSendWorkspaceInviteWorkflowInputCarriesNoCredential enforces AGENTS.md Rule #10:
// invite workflow inputs must never carry raw tokens, URLs, or secrets in Temporal history.
func TestSendWorkspaceInviteWorkflowInputCarriesNoCredential(t *testing.T) {
	typ := reflect.TypeOf(activities.InviteEmailInput{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		lower := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
		for _, forbidden := range []string{"token", "url", "secret", "password", "key"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("InviteEmailInput field %s (%q) looks like a credential; keep secrets in DB sealed by Vault", f.Name, f.Tag.Get("json"))
			}
		}
	}
}

func TestSendWorkspaceInviteWorkflowRetries503AndClearsPendingToken(t *testing.T) {
	adminPool, appPool := openClaimTestDB(t)
	ctx := context.Background()

	wsID, userID, invID := uuid.New(), uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name, owner_user_id) VALUES ($1, 'Invite Workflow WS', $2)", wsID, userID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	vault := newClaimVault(t)
	rawToken := "super-secret-raw-invite-token"
	hashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hashBytes[:])
	sealed, err := vault.Encrypt(rawToken)
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workspace_invitations (
			id, workspace_id, workspace_name, invited_by_user_id, invited_by_email,
			email, role, token_hash, encrypted_pending_token, status, expires_at
		) VALUES ($1, $2, 'Kingdon Household', $3, 'chris@example.com', 'jane@example.com', 'member', $4, $5, 'PENDING', $6)
	`, invID, wsID, userID, tokenHash, sealed, time.Now().UTC().Add(7*24*time.Hour)); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}

	var attempts atomic.Int32
	var seenKeys []string
	var sentHTML string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKeys = append(seenKeys, r.Header.Get("Idempotency-Key"))
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporary outage"))
			return
		}
		var payload struct {
			HTML string `json:"html"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sentHTML = payload.HTML
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_ok_1"}`))
	}))
	defer srv.Close()

	sender := email.NewResendSender("re_test", "Budget App <invites@example.com>", srv.Client())
	sender.BaseURL = srv.URL

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	acts := &activities.InviteActivities{
		Pool:         appPool,
		Vault:        vault,
		Sender:       sender,
		AppPublicURL: "https://budget.example.com",
	}
	env.RegisterActivity(acts)

	env.ExecuteWorkflow(SendWorkspaceInviteWorkflow, activities.InviteEmailInput{
		SyncScope:    temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID},
		InvitationID: invID,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed after 503 retry: %v", err)
	}

	var res activities.InviteEmailResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.MessageID != "msg_ok_1" {
		t.Fatalf("MessageID = %q, want msg_ok_1", res.MessageID)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts (503 then 200), got %d", attempts.Load())
	}
	if len(seenKeys) != 2 || seenKeys[0] == "" || seenKeys[0] != seenKeys[1] {
		t.Fatalf("expected identical non-empty Idempotency-Key across retries, got %v", seenKeys)
	}
	if !strings.Contains(sentHTML, "https://budget.example.com/invite?token="+rawToken) {
		t.Fatalf("expected invite URL in email HTML, got: %s", sentHTML)
	}

	// Verify encrypted_pending_token was cleared once sent
	var pending *string
	if err := adminPool.QueryRow(ctx, `SELECT encrypted_pending_token FROM workspace_invitations WHERE id = $1`, invID).Scan(&pending); err != nil {
		t.Fatalf("query pending token: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected encrypted_pending_token to be cleared after send, got %q", *pending)
	}
}

func TestSendWorkspaceInviteWorkflowDoesNotRetry400(t *testing.T) {
	adminPool, appPool := openClaimTestDB(t)
	ctx := context.Background()

	wsID, userID, invID := uuid.New(), uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspaces (id, name, owner_user_id) VALUES ($1, 'Invite 400 WS', $2)", wsID, userID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	vault := newClaimVault(t)
	hashBytes := sha256.Sum256([]byte("bad-email-token"))
	sealed, _ := vault.Encrypt("bad-email-token")
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workspace_invitations (
			id, workspace_id, workspace_name, invited_by_user_id, invited_by_email,
			email, role, token_hash, encrypted_pending_token, status, expires_at
		) VALUES ($1, $2, 'Household', $3, 'chris@example.com', 'invalid', 'member', $4, $5, 'PENDING', $6)
	`, invID, wsID, userID, hex.EncodeToString(hashBytes[:]), sealed, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid recipient"}`))
	}))
	defer srv.Close()

	sender := email.NewResendSender("re_test", "Budget App <invites@example.com>", srv.Client())
	sender.BaseURL = srv.URL

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.InviteActivities{
		Pool:         appPool,
		Vault:        vault,
		Sender:       sender,
		AppPublicURL: "https://budget.example.com",
	})

	env.ExecuteWorkflow(SendWorkspaceInviteWorkflow, activities.InviteEmailInput{
		SyncScope:    temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID},
		InvitationID: invID,
	})
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected workflow to fail on 400")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("expected non-retryable ApplicationError, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("expected 1 attempt on terminal 400, got %d", attempts.Load())
	}
}
