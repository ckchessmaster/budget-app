package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/admin"
	"github.com/google/uuid"
)

type stubTemporalHealth struct {
	err error
}

func (s stubTemporalHealth) CheckHealth(_ context.Context) error {
	return s.err
}

type stubVisibilityClient struct {
	counts admin.FailedWorkflowCounts
	err    error
}

func (s stubVisibilityClient) ListFailedWorkflowsSince(_ context.Context, _ time.Time) (admin.FailedWorkflowCounts, error) {
	return s.counts, s.err
}

func TestAdminHealth(t *testing.T) {
	adminPool, appPool := openAdminTestPools(t)
	ctx := context.Background()

	adminID := uuid.New()
	wsID := uuid.New()
	adminEmail := "health-admin-" + adminID.String()[:8] + "@example.com"

	_, err := adminPool.Exec(ctx, `
		INSERT INTO platform_users (user_id, email, display_name, is_active)
		VALUES ($1, $2, 'Health Admin', TRUE)
	`, adminID, adminEmail)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = adminPool.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo, owner_user_id)
		VALUES ($1, 'Health Test Budget', FALSE, $2)
	`, wsID, adminID)
	if err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	_, err = adminPool.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, adminID)
	if err != nil {
		t.Fatalf("seed member: %v", err)
	}
	_, err = adminPool.Exec(ctx, `
		INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, last_synced_at, last_sync_status)
		VALUES ($1, 'enc', NOW() - INTERVAL '1 hour', 'OK')
	`, wsID)
	if err != nil {
		t.Fatalf("seed simplefin_connections: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM outbound_email_logs WHERE template = 'health_test'")
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE id = $1", wsID)
		_, _ = adminPool.Exec(bg, "DELETE FROM platform_users WHERE user_id = $1", adminID)
	})

	visStub := &stubVisibilityClient{}
	h := admin.NewHandler(appPool, newTestVault(t), nil, nil, visStub, stubTemporalHealth{}, "https://budget.example.com")
	router := setupAdminRouter(h, adminID, adminEmail)

	// 1. Healthy baseline (unless another test left stale rows; verify our workspace is not in stale_or_errored)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/api/v1/admin/health", nil))
	if rec1.Code != http.StatusOK {
		t.Fatalf("GetHealth = %d: %s", rec1.Code, rec1.Body.String())
	}
	var resp1 admin.HealthResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if resp1.Dependencies["postgres"].Status != "ok" || resp1.Dependencies["temporal"].Status != "ok" {
		t.Fatalf("unexpected dependencies: %+v", resp1.Dependencies)
	}
	if resp1.SimpleFIN.ConfiguredWorkspaces < 1 {
		t.Fatalf("expected >= 1 configured workspace, got %d", resp1.SimpleFIN.ConfiguredWorkspaces)
	}

	// 2. Stale (> 12h) SimpleFIN sync triggers degraded and lists workspace
	if _, err := adminPool.Exec(ctx, `
		UPDATE simplefin_connections
		SET last_synced_at = NOW() - INTERVAL '14 hours',
		    last_sync_status = 'ERROR',
		    last_sync_error = 'HTTP 403: Access token revoked'
		WHERE workspace_id = $1
	`, wsID); err != nil {
		t.Fatalf("update simplefin_connections: %v", err)
	}

	visStub.counts = admin.FailedWorkflowCounts{
		FailedBankSyncs: 2,
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO outbound_email_logs (template, status, error_message)
		VALUES ('health_test', 'SENT', NULL), ('health_test', 'FAILED', 'rate limited')
	`); err != nil {
		t.Fatalf("seed outbound_email_logs: %v", err)
	}

	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/admin/health", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("GetHealth degraded = %d: %s", rec2.Code, rec2.Body.String())
	}
	var resp2 admin.HealthResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
	if resp2.Status != "degraded" {
		t.Fatalf("Status = %q, want degraded", resp2.Status)
	}
	if resp2.TemporalWorkflows24h.FailedBankSyncs != 2 {
		t.Fatalf("FailedBankSyncs = %d, want 2", resp2.TemporalWorkflows24h.FailedBankSyncs)
	}
	if resp2.OutboundEmail24h.Sent < 1 || resp2.OutboundEmail24h.Failed < 1 {
		t.Fatalf("unexpected OutboundEmail24h: %+v", resp2.OutboundEmail24h)
	}
	foundStale := false
	for _, s := range resp2.SimpleFIN.StaleOrErrored {
		if s.WorkspaceID == wsID && s.Status == "ERROR" {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("expected workspace %s in stale_or_errored: %+v", wsID, resp2.SimpleFIN.StaleOrErrored)
	}
}
