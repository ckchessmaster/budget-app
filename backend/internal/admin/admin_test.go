package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/admin"
	"github.com/ckchessmaster/budget-app/backend/internal/idp"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openAdminTestPools(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
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
		t.Skipf("skipping admin DB test: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping admin DB test (postgres unreachable): %v", err)
	}
	appPool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping admin DB test (app connect): %v", err)
	}
	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})
	return adminPool, appPool
}

type mockIDPAdmin struct {
	mu           sync.Mutex
	createdCalls int
	activeCalls  []struct {
		id     uuid.UUID
		active bool
	}
	deletedIDs  []uuid.UUID
	takenEmails map[string]bool
	takenUsers  map[string]bool
	createDelay time.Duration
}

func (m *mockIDPAdmin) CreateUserAndRecoveryLink(_ context.Context, email, _ string, username string) (uuid.UUID, string, error) {
	if m.createDelay > 0 {
		time.Sleep(m.createDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.takenEmails[email] {
		return uuid.Nil, "", idp.ErrEmailAlreadyExists
	}
	if m.takenUsers[username] {
		return uuid.Nil, "", idp.ErrUsernameAlreadyExists
	}
	m.createdCalls++
	id := uuid.New()
	return id, "https://auth.example.com/recovery/" + id.String(), nil
}

func (m *mockIDPAdmin) SetUserActive(_ context.Context, userUUID uuid.UUID, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeCalls = append(m.activeCalls, struct {
		id     uuid.UUID
		active bool
	}{id: userUUID, active: active})
	return nil
}

func (m *mockIDPAdmin) DeleteUser(_ context.Context, userUUID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedIDs = append(m.deletedIDs, userUUID)
	return nil
}

type mockPlatformInviteWF struct {
	started []uuid.UUID
}

func (m *mockPlatformInviteWF) Start(_ context.Context, invitationID uuid.UUID) (string, error) {
	m.started = append(m.started, invitationID)
	return "platform-invite-" + invitationID.String(), nil
}

func newTestVault(t *testing.T) crypto.Vault {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	v, err := crypto.NewAESGCMVault(key)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	return v
}

func setupAdminRouter(h *admin.Handler, adminID uuid.UUID, adminEmail string) chi.Router {
	r := chi.NewRouter()
	admin.RegisterPublicRoutes(r, h)
	r.Group(func(adm chi.Router) {
		adm.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := middleware.WithUserContext(req.Context(), adminID, adminEmail, "Platform Admin", []string{"budget-admins"})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		admin.RegisterAdminRoutes(adm, h)
	})
	return r
}

func TestPlatformInvitesAndSignupRedemption(t *testing.T) {
	adminPool, appPool := openAdminTestPools(t)
	ctx := context.Background()

	adminID := uuid.New()
	adminEmail := "admin-" + adminID.String()[:8] + "@example.com"
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO platform_users (user_id, email, display_name, is_active)
		VALUES ($1, $2, 'Platform Admin', TRUE)
	`, adminID, adminEmail); err != nil {
		t.Fatalf("seed admin user: %v", err)
	}

	var createdUserIDs []uuid.UUID
	var createdInviteIDs []uuid.UUID
	t.Cleanup(func() {
		bg := context.Background()
		for _, id := range createdInviteIDs {
			_, _ = adminPool.Exec(bg, "DELETE FROM platform_invitations WHERE id = $1", id)
		}
		for _, uid := range createdUserIDs {
			_, _ = adminPool.Exec(bg, "DELETE FROM platform_users WHERE user_id = $1", uid)
		}
		_, _ = adminPool.Exec(bg, "DELETE FROM platform_users WHERE user_id = $1", adminID)
	})

	idpMock := &mockIDPAdmin{
		takenEmails: map[string]bool{"duplicate@example.com": true},
		takenUsers:  map[string]bool{"takenname": true},
	}
	wfMock := &mockPlatformInviteWF{}
	h := admin.NewHandler(appPool, newTestVault(t), idpMock, wfMock, nil, nil, "https://budget.example.com")
	router := setupAdminRouter(h, adminID, adminEmail)

	// 1. Admin creates an invite with optional email -> dispatches workflow and returns invite_url
	reqBody, _ := json.Marshal(map[string]any{
		"label":            "For Mike",
		"expires_in_hours": 168,
		"email":            "mike@example.com",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/invitations", bytes.NewReader(reqBody))
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("CreateInvitation = %d: %s", createRec.Code, createRec.Body.String())
	}

	var created admin.PlatformInvitationDTO
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdInviteIDs = append(createdInviteIDs, created.ID)

	if !created.EmailDispatched || len(wfMock.started) != 1 || wfMock.started[0] != created.ID {
		t.Fatalf("expected email workflow started for %s, got dispatched=%v started=%v", created.ID, created.EmailDispatched, wfMock.started)
	}
	parsedURL, err := url.Parse(created.InviteURL)
	if err != nil {
		t.Fatalf("parse invite url: %v", err)
	}
	rawToken := parsedURL.Query().Get("invite")
	if rawToken == "" {
		t.Fatalf("missing invite query param in %q", created.InviteURL)
	}

	// 2. Public validate returns valid + prefilled email
	valReq := httptest.NewRequest(http.MethodGet, "/api/v1/signup/validate?invite="+url.QueryEscape(rawToken), nil)
	valRec := httptest.NewRecorder()
	router.ServeHTTP(valRec, valReq)
	if valRec.Code != http.StatusOK {
		t.Fatalf("ValidateSignupInvite = %d: %s", valRec.Code, valRec.Body.String())
	}
	var valBody map[string]any
	_ = json.Unmarshal(valRec.Body.Bytes(), &valBody)
	if valBody["valid"] != true || valBody["email"] != "mike@example.com" {
		t.Fatalf("unexpected validate body: %+v", valBody)
	}

	// 3. Duplicate username returns 409 WITHOUT consuming the token
	dupBody, _ := json.Marshal(map[string]string{
		"token":    rawToken,
		"email":    "mike@example.com",
		"name":     "Mike",
		"username": "takenname",
	})
	dupReq := httptest.NewRequest(http.MethodPost, "/api/v1/signup/redeem", bytes.NewReader(dupBody))
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dupReq)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("duplicate username status = %d, want 409: %s", dupRec.Code, dupRec.Body.String())
	}

	// Token must still validate as ACTIVE
	valRec2 := httptest.NewRecorder()
	router.ServeHTTP(valRec2, httptest.NewRequest(http.MethodGet, "/api/v1/signup/validate?invite="+url.QueryEscape(rawToken), nil))
	if valRec2.Code != http.StatusOK {
		t.Fatalf("expected token to remain valid after failed duplicate username, got %d", valRec2.Code)
	}

	// 4. Race-safety: two concurrent redemptions of the same token -> exactly one 201 and one 409
	idpMock.createDelay = 25 * time.Millisecond
	var okCount, conflictCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{
				"token":    rawToken,
				"email":    "mike" + string(rune('a'+idx)) + "@example.com",
				"name":     "Mike",
				"username": "mike" + string(rune('a'+idx)),
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/signup/redeem", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			switch rec.Code {
			case http.StatusCreated:
				okCount.Add(1)
			case http.StatusConflict:
				conflictCount.Add(1)
			default:
				t.Errorf("unexpected status %d: %s", rec.Code, rec.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if okCount.Load() != 1 || conflictCount.Load() != 1 {
		t.Fatalf("race result: 201=%d, 409=%d (want 1 and 1)", okCount.Load(), conflictCount.Load())
	}

	var redeemedUserID uuid.UUID
	if err := adminPool.QueryRow(ctx, "SELECT redeemed_by_user_id FROM platform_invitations WHERE id = $1", created.ID).Scan(&redeemedUserID); err != nil {
		t.Fatalf("query redeemed_by_user_id: %v", err)
	}
	createdUserIDs = append(createdUserIDs, redeemedUserID)

	// 5. Revoking a redeemed invite returns 409; revoking an active invite returns 204
	revRedeemedRec := httptest.NewRecorder()
	router.ServeHTTP(revRedeemedRec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/invitations/"+created.ID.String(), nil))
	if revRedeemedRec.Code != http.StatusConflict {
		t.Fatalf("revoke redeemed = %d, want 409", revRedeemedRec.Code)
	}

	reqBody2, _ := json.Marshal(map[string]any{"label": "Revokable"})
	createRec2 := httptest.NewRecorder()
	router.ServeHTTP(createRec2, httptest.NewRequest(http.MethodPost, "/api/v1/admin/invitations", bytes.NewReader(reqBody2)))
	var created2 admin.PlatformInvitationDTO
	_ = json.Unmarshal(createRec2.Body.Bytes(), &created2)
	createdInviteIDs = append(createdInviteIDs, created2.ID)

	revRec := httptest.NewRecorder()
	router.ServeHTTP(revRec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/invitations/"+created2.ID.String(), nil))
	if revRec.Code != http.StatusNoContent {
		t.Fatalf("revoke active = %d, want 204: %s", revRec.Code, revRec.Body.String())
	}
}

func TestAdminUserLifecycleAndWorkspaceOversight(t *testing.T) {
	adminPool, appPool := openAdminTestPools(t)
	ctx := context.Background()

	adminID := uuid.New()
	friendID := uuid.New()
	friendWS := uuid.New()
	adminEmail := "admin-lc-" + adminID.String()[:8] + "@example.com"
	friendEmail := "friend-lc-" + friendID.String()[:8] + "@example.com"

	_, err := adminPool.Exec(ctx, `
		INSERT INTO platform_users (user_id, email, display_name, is_active)
		VALUES ($1, $2, 'Admin', TRUE), ($3, $4, 'Friend', TRUE)
	`, adminID, adminEmail, friendID, friendEmail)
	if err != nil {
		t.Fatalf("seed users: %v", err)
	}
	_, err = adminPool.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo, owner_user_id)
		VALUES ($1, 'Friend Budget', FALSE, $2)
	`, friendWS, friendID)
	if err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	_, err = adminPool.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, friendWS, friendID)
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE id = $1", friendWS)
		_, _ = adminPool.Exec(bg, "DELETE FROM platform_users WHERE user_id IN ($1, $2)", adminID, friendID)
	})

	idpMock := &mockIDPAdmin{}
	h := admin.NewHandler(appPool, newTestVault(t), idpMock, nil, nil, nil, "https://budget.example.com")
	router := setupAdminRouter(h, adminID, adminEmail)

	// 1. Self-disable & self-delete guards return 400
	disableSelfBody, _ := json.Marshal(map[string]bool{"is_active": false})
	selfDisRec := httptest.NewRecorder()
	router.ServeHTTP(selfDisRec, httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+adminID.String()+"/status", bytes.NewReader(disableSelfBody)))
	if selfDisRec.Code != http.StatusBadRequest {
		t.Fatalf("self-disable = %d, want 400", selfDisRec.Code)
	}

	selfDelRec := httptest.NewRecorder()
	router.ServeHTTP(selfDelRec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+adminID.String(), nil))
	if selfDelRec.Code != http.StatusBadRequest {
		t.Fatalf("self-delete = %d, want 400", selfDelRec.Code)
	}

	// 2. Disable friend syncs to Authentik & platform_users
	disRec := httptest.NewRecorder()
	router.ServeHTTP(disRec, httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+friendID.String()+"/status", bytes.NewReader(disableSelfBody)))
	if disRec.Code != http.StatusOK {
		t.Fatalf("disable friend = %d: %s", disRec.Code, disRec.Body.String())
	}
	var isActive bool
	_ = adminPool.QueryRow(ctx, "SELECT is_active FROM platform_users WHERE user_id = $1", friendID).Scan(&isActive)
	if isActive {
		t.Fatal("expected friend is_active = false in DB")
	}
	if len(idpMock.activeCalls) != 1 || idpMock.activeCalls[0].id != friendID || idpMock.activeCalls[0].active {
		t.Fatalf("unexpected IDP active calls: %+v", idpMock.activeCalls)
	}

	// 3. ListWorkspaces exposes metadata including owner email
	wsRec := httptest.NewRecorder()
	router.ServeHTTP(wsRec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/workspaces", nil))
	if wsRec.Code != http.StatusOK {
		t.Fatalf("ListWorkspaces = %d: %s", wsRec.Code, wsRec.Body.String())
	}
	var wsList []admin.AdminWorkspaceOverviewDTO
	_ = json.Unmarshal(wsRec.Body.Bytes(), &wsList)
	found := false
	for _, w := range wsList {
		if w.WorkspaceID == friendWS && w.OwnerEmail == friendEmail && w.SimpleFINStatus == "NOT_CONFIGURED" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected friend workspace metadata in %+v", wsList)
	}

	// 4. Delete friend deletes in Authentik and cascades sole-owned workspace
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+friendID.String(), nil))
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DeleteUser = %d: %s", delRec.Code, delRec.Body.String())
	}
	if len(idpMock.deletedIDs) != 1 || idpMock.deletedIDs[0] != friendID {
		t.Fatalf("unexpected IDP deletedIDs: %+v", idpMock.deletedIDs)
	}
	var wsExists bool
	_ = adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM workspaces WHERE id = $1)", friendWS).Scan(&wsExists)
	if wsExists {
		t.Fatal("expected friend's sole-owned workspace to be deleted on user deletion")
	}
}
