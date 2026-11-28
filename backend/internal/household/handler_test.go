package household_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/household"
	"github.com/ckchessmaster/budget-app/backend/internal/idp"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openHouseholdTestPools(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
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
		t.Skipf("skipping household DB test: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping household DB test (postgres unreachable): %v", err)
	}
	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping household DB test (app connect): %v", err)
	}
	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})
	return adminPool, appPool
}

type stubVerifier struct {
	users map[string]middleware.TokenClaims
}

func (v *stubVerifier) Verify(_ context.Context, rawToken string) (string, error) {
	c, ok := v.users[rawToken]
	if !ok {
		return "", middleware.ErrInvalidToken
	}
	return c.Subject, nil
}

func (v *stubVerifier) VerifyClaims(_ context.Context, rawToken string) (middleware.TokenClaims, error) {
	c, ok := v.users[rawToken]
	if !ok {
		return middleware.TokenClaims{}, middleware.ErrInvalidToken
	}
	return c, nil
}

type stubInviteWorkflow struct {
	started []uuid.UUID
}

func (s *stubInviteWorkflow) Start(_ context.Context, scope temporalinternal.SyncScope, invitationID uuid.UUID) (string, error) {
	s.started = append(s.started, invitationID)
	return "workspace-invite-" + scope.WorkspaceID.String() + "-" + invitationID.String(), nil
}

type stubIDP struct {
	user idp.ProvisionedUser
}

func (s *stubIDP) EnsureUser(_ context.Context, email string, displayName string) (idp.ProvisionedUser, error) {
	u := s.user
	u.Email = email
	if u.DisplayName == "" {
		u.DisplayName = displayName
	}
	return u, nil
}

func decryptInviteTokenFromDB(t *testing.T, adminPool *pgxpool.Pool, vault crypto.Vault, invID uuid.UUID) string {
	t.Helper()
	var sealed string
	if err := adminPool.QueryRow(context.Background(), `
		SELECT COALESCE(encrypted_pending_token, '') FROM workspace_invitations WHERE id = $1
	`, invID).Scan(&sealed); err != nil {
		t.Fatalf("read staged token: %v", err)
	}
	if sealed == "" {
		t.Fatal("expected staged encrypted_pending_token to be non-empty")
	}
	raw, err := vault.Decrypt(sealed)
	if err != nil {
		t.Fatalf("decrypt staged token: %v", err)
	}
	return raw
}

func TestHouseholdInvitationsEndToEnd(t *testing.T) {
	adminPool, appPool := openHouseholdTestPools(t)
	ctx := context.Background()

	ownerID := uuid.New()
	inviteeID := uuid.New()
	otherUserID := uuid.New()
	wsID := uuid.New()

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo, owner_user_id)
		VALUES ($1, 'Kingdon Household', FALSE, $2)
	`, wsID, ownerID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, ownerID); err != nil {
		t.Fatalf("seed owner member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE owner_user_id IN ($1, $2)", ownerID, inviteeID)
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM workspaces WHERE id = $1", wsID)
	})

	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	prov := workspace.NewProvisioner()
	wfStub := &stubInviteWorkflow{}
	idpStub := &stubIDP{
		user: idp.ProvisionedUser{
			UserUUID:         inviteeID,
			IsNewUser:        true,
			PasswordSetupURL: "https://authentik.example.com/recovery/one-time-link",
		},
	}
	verifier := &stubVerifier{
		users: map[string]middleware.TokenClaims{
			"owner-jwt": {Subject: ownerID.String(), Email: "chris@example.com", Name: "Chris Kingdon"},
			"jane-jwt":  {Subject: inviteeID.String(), Email: "jane@example.com", Name: "Jane Kingdon"},
			"other-jwt": {Subject: otherUserID.String(), Email: "someoneelse@example.com", Name: "Other"},
		},
	}

	h := household.NewHandler(appPool, vault, prov, idpStub, verifier, wfStub, nil, "https://budget.example.com")
	r := chi.NewRouter()
	household.RegisterPublicRoutes(r, h)
	r.Group(func(authRouter chi.Router) {
		authRouter.Use(middleware.AuthMiddleware(verifier))
		household.RegisterUserRoutes(authRouter, h)
		authRouter.Group(func(wsRouter chi.Router) {
			wsRouter.Use(middleware.RequireWorkspace(appPool, prov))
			household.RegisterWorkspaceRoutes(wsRouter, h)
		})
	})

	doJSON := func(method, path, token, wsHeader string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if wsHeader != "" {
			req.Header.Set("X-Workspace-ID", wsHeader)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// 1. Owner creates an invitation -> Temporal workflow dispatched & raw token omitted from response
	createRec := doJSON(http.MethodPost, "/api/v1/workspace/invitations", "owner-jwt", wsID.String(), map[string]string{
		"email": "jane@example.com",
		"role":  "member",
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create invite = %d (%s), want 201", createRec.Code, createRec.Body.String())
	}
	if strings.Contains(createRec.Body.String(), "token") {
		t.Fatalf("create invite response leaked token field: %s", createRec.Body.String())
	}
	var createdInv household.InvitationDTO
	_ = json.Unmarshal(createRec.Body.Bytes(), &createdInv)
	if len(wfStub.started) != 1 || wfStub.started[0] != createdInv.ID {
		t.Fatalf("expected workflow dispatch for %s, got %v", createdInv.ID, wfStub.started)
	}

	firstRawToken := decryptInviteTokenFromDB(t, adminPool, vault, createdInv.ID)

	// 2. Resend rotates token_hash and resets expiry
	resendRec := doJSON(http.MethodPost, "/api/v1/workspace/invitations/"+createdInv.ID.String()+"/resend", "owner-jwt", wsID.String(), nil)
	if resendRec.Code != http.StatusOK {
		t.Fatalf("resend invite = %d (%s), want 200", resendRec.Code, resendRec.Body.String())
	}
	if len(wfStub.started) != 2 {
		t.Fatalf("expected 2 workflow dispatches after resend, got %d", len(wfStub.started))
	}
	rotatedRawToken := decryptInviteTokenFromDB(t, adminPool, vault, createdInv.ID)
	if rotatedRawToken == firstRawToken {
		t.Fatal("expected resend to rotate raw token")
	}

	// Old token must now 404 on preview
	oldPreview := doJSON(http.MethodGet, "/api/v1/invitations/preview?token="+url.QueryEscape(firstRawToken), "", "", nil)
	if oldPreview.Code != http.StatusNotFound {
		t.Fatalf("old token preview = %d, want 404", oldPreview.Code)
	}

	// 3. Preview with mismatched signed-in JWT reports email_mismatch=true, and accept returns 403
	mismatchPreview := doJSON(http.MethodGet, "/api/v1/invitations/preview?token="+url.QueryEscape(rotatedRawToken), "other-jwt", "", nil)
	if mismatchPreview.Code != http.StatusOK {
		t.Fatalf("mismatch preview = %d (%s)", mismatchPreview.Code, mismatchPreview.Body.String())
	}
	var prevDTO household.InvitePreviewDTO
	_ = json.Unmarshal(mismatchPreview.Body.Bytes(), &prevDTO)
	if !prevDTO.EmailMismatch || prevDTO.WorkspaceName != "Kingdon Household" {
		t.Fatalf("unexpected preview DTO: %+v", prevDTO)
	}

	mismatchAccept := doJSON(http.MethodPost, "/api/v1/invitations/accept", "other-jwt", "", map[string]string{
		"token": rotatedRawToken,
		"name":  "Someone Else",
	})
	if mismatchAccept.Code != http.StatusForbidden {
		t.Fatalf("email mismatch accept = %d (%s), want 403", mismatchAccept.Code, mismatchAccept.Body.String())
	}

	// 4. Valid accept provisions net-new Authentik user, inserts workspace_members + roster, returns redirect_url
	acceptRec := doJSON(http.MethodPost, "/api/v1/invitations/accept", "", "", map[string]string{
		"token": rotatedRawToken,
		"name":  "Jane Kingdon",
	})
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept invite = %d (%s), want 200", acceptRec.Code, acceptRec.Body.String())
	}
	var acceptResp household.AcceptInviteResponse
	_ = json.Unmarshal(acceptRec.Body.Bytes(), &acceptResp)
	if acceptResp.WorkspaceID != wsID || !acceptResp.IsNewUser || acceptResp.RedirectURL != "https://authentik.example.com/recovery/one-time-link" {
		t.Fatalf("unexpected accept response: %+v", acceptResp)
	}

	// Re-accepting the same token returns 409 Conflict
	reAcceptRec := doJSON(http.MethodPost, "/api/v1/invitations/accept", "", "", map[string]string{
		"token": rotatedRawToken,
		"name":  "Jane Kingdon",
	})
	if reAcceptRec.Code != http.StatusConflict {
		t.Fatalf("re-accept = %d, want 409", reAcceptRec.Code)
	}

	// 5. Invitee creates their own personal workspace once, and second attempt returns 409
	createPersonal := doJSON(http.MethodPost, "/api/v1/workspaces", "jane-jwt", "", map[string]string{
		"name": "Jane's Personal Budget",
	})
	if createPersonal.Code != http.StatusCreated {
		t.Fatalf("create personal workspace = %d (%s), want 201", createPersonal.Code, createPersonal.Body.String())
	}
	secondPersonal := doJSON(http.MethodPost, "/api/v1/workspaces", "jane-jwt", "", map[string]string{
		"name": "Another Budget",
	})
	if secondPersonal.Code != http.StatusConflict {
		t.Fatalf("second personal workspace = %d (%s), want 409", secondPersonal.Code, secondPersonal.Body.String())
	}

	// GET /api/v1/workspaces returns both workspaces for Jane
	wsListRec := doJSON(http.MethodGet, "/api/v1/workspaces", "jane-jwt", "", nil)
	if wsListRec.Code != http.StatusOK {
		t.Fatalf("list workspaces = %d", wsListRec.Code)
	}
	var summaries []workspace.WorkspaceSummary
	_ = json.Unmarshal(wsListRec.Body.Bytes(), &summaries)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 workspaces for Jane, got %d (%s)", len(summaries), wsListRec.Body.String())
	}

	// 6. Owner changes Jane's role to read_only; member list reflects it; owner cannot remove self
	roleRec := doJSON(http.MethodPatch, "/api/v1/workspace/members/"+inviteeID.String(), "owner-jwt", wsID.String(), map[string]string{
		"role": "read_only",
	})
	if roleRec.Code != http.StatusOK {
		t.Fatalf("update role = %d (%s)", roleRec.Code, roleRec.Body.String())
	}

	selfRemoveOwner := doJSON(http.MethodDelete, "/api/v1/workspace/members/"+ownerID.String(), "owner-jwt", wsID.String(), nil)
	if selfRemoveOwner.Code != http.StatusBadRequest {
		t.Fatalf("owner self-remove = %d, want 400", selfRemoveOwner.Code)
	}

	// Jane self-leaves the household workspace -> 204
	selfLeave := doJSON(http.MethodDelete, "/api/v1/workspace/members/"+inviteeID.String(), "jane-jwt", wsID.String(), nil)
	if selfLeave.Code != http.StatusNoContent {
		t.Fatalf("member self-leave = %d (%s), want 204", selfLeave.Code, selfLeave.Body.String())
	}

	// 7. Expired invitation returns 410 Gone
	expInvRec := doJSON(http.MethodPost, "/api/v1/workspace/invitations", "owner-jwt", wsID.String(), map[string]string{
		"email": "expired@example.com",
		"role":  "read_only",
	})
	if expInvRec.Code != http.StatusCreated {
		t.Fatalf("create expired test invite = %d (%s)", expInvRec.Code, expInvRec.Body.String())
	}
	var expInv household.InvitationDTO
	_ = json.Unmarshal(expInvRec.Body.Bytes(), &expInv)
	expToken := decryptInviteTokenFromDB(t, adminPool, vault, expInv.ID)
	if _, err := adminPool.Exec(ctx, `UPDATE workspace_invitations SET expires_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, expInv.ID); err != nil {
		t.Fatalf("backdate expiry: %v", err)
	}
	expAccept := doJSON(http.MethodPost, "/api/v1/invitations/accept", "", "", map[string]string{
		"token": expToken,
		"name":  "Late User",
	})
	if expAccept.Code != http.StatusGone {
		t.Fatalf("expired accept = %d (%s), want 410", expAccept.Code, expAccept.Body.String())
	}

	// Reference activities package so unused import check doesn't trigger if refactored
	_ = activities.InviteEmailInput{}
	_ = db.WithWorkspaceID
}
