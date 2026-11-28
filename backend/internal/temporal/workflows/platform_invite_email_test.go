package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/email"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/testsuite"
)

func TestSendPlatformInviteWorkflowInputCarriesNoCredential(t *testing.T) {
	typ := reflect.TypeOf(activities.PlatformInviteEmailInput{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		lower := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
		for _, forbidden := range []string{"token", "url", "secret", "password", "key", "email"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("PlatformInviteEmailInput field %s (%q) looks like a credential or PII; keep in DB", f.Name, f.Tag.Get("json"))
			}
		}
	}
}

func TestSendPlatformInviteWorkflow_SendsAndClearsPendingToken(t *testing.T) {
	adminPool, appPool := openClaimTestDB(t)
	ctx := context.Background()

	adminID, invID := uuid.New(), uuid.New()
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform_users (user_id, email, display_name, is_active)
		VALUES ($1, $2, 'Admin User', TRUE)
	`, adminID, "admin-"+adminID.String()[:8]+"@example.com"); err != nil {
		t.Fatalf("seed admin platform_user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM platform_invitations WHERE id = $1", invID)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM platform_users WHERE user_id = $1", adminID)
	})

	vault := newClaimVault(t)
	rawToken := "platform-raw-secret-token"
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	sealed, err := vault.Encrypt(rawToken)
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform_invitations (
			id, label, recipient_email, token_hash, encrypted_pending_token,
			status, created_by_user_id, expires_at
		) VALUES ($1, 'For Mike', 'mike@example.com', $2, $3, 'ACTIVE', $4, $5)
	`, invID, tokenHash, sealed, adminID, time.Now().UTC().Add(7*24*time.Hour)); err != nil {
		t.Fatalf("seed platform invitation: %v", err)
	}

	var sentHTML string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			HTML string `json:"html"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sentHTML = payload.HTML
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"plat_msg_1"}`))
	}))
	defer srv.Close()

	sender := email.NewResendSender("re_test", "Budget App <invites@example.com>", srv.Client())
	sender.BaseURL = srv.URL

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&activities.PlatformInviteActivities{
		Pool:         appPool,
		Vault:        vault,
		Sender:       sender,
		AppPublicURL: "https://budget.example.com",
	})

	env.ExecuteWorkflow(SendPlatformInviteWorkflow, activities.PlatformInviteEmailInput{
		InvitationID: invID,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if !strings.Contains(sentHTML, "https://budget.example.com/signup?invite="+rawToken) {
		t.Fatalf("expected signup URL in email HTML, got: %s", sentHTML)
	}

	var pending *string
	if err := adminPool.QueryRow(ctx, "SELECT encrypted_pending_token FROM platform_invitations WHERE id = $1", invID).Scan(&pending); err != nil {
		t.Fatalf("query pending token: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected encrypted_pending_token to be cleared, got %q", *pending)
	}
}
