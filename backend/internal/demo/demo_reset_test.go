package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
)

type fakeResetStarter struct {
	calls       int
	scope       temporalinternal.SyncScope
	triggerSync bool
	workflowID  string
	err         error
}

func (f *fakeResetStarter) Start(_ context.Context, scope temporalinternal.SyncScope, triggerSync bool) (string, error) {
	f.calls++
	f.scope = scope
	f.triggerSync = triggerSync
	if f.err != nil {
		return "", f.err
	}
	return f.workflowID, nil
}

func postDemoReset(t *testing.T, h *Handler, userID uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/reset", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ResetDemo(rec, req)
	return rec
}

func TestResetDemoAcceptsAndHandsOffToTheWorkflow(t *testing.T) {
	_, appPool, userID := openDemoTestDB(t)

	provisioner := workspace.NewProvisioner()
	starter := &fakeResetStarter{workflowID: "demo-reset-ws-1"}
	h := NewHandler(appPool, testVault(t), "http://127.0.0.1:8080", provisioner, nil, starter)

	rec := postDemoReset(t, h, userID, `{"trigger_sync":true}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["workflow_id"] != "demo-reset-ws-1" || body["status"] != "accepted" {
		t.Fatalf("body = %v, want the workflow id and accepted", body)
	}
	if starter.calls != 1 || starter.scope.UserID != userID || starter.scope.WorkspaceID == uuid.Nil {
		t.Fatalf("starter received %d calls with scope %+v", starter.calls, starter.scope)
	}
	if !starter.triggerSync {
		t.Fatalf("trigger_sync was not passed through")
	}
}

// TestResetDemoRejectsAConcurrentReset is the acceptance criterion for one reset at a
// time. Two resets interleaving delete and seed against the same sandbox is how the
// sandbox ends up half-built, so the second caller is told to wait instead of queueing.
func TestResetDemoRejectsAConcurrentReset(t *testing.T) {
	_, appPool, userID := openDemoTestDB(t)

	provisioner := workspace.NewProvisioner()
	starter := &fakeResetStarter{err: temporalinternal.ErrWorkflowAlreadyRunning}
	h := NewHandler(appPool, testVault(t), "http://127.0.0.1:8080", provisioner, nil, starter)

	rec := postDemoReset(t, h, userID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "demo_reset_already_running") {
		t.Fatalf("body = %s, want demo_reset_already_running", rec.Body.String())
	}
}

func TestResetDemoReportsMissingTemporal(t *testing.T) {
	_, appPool, userID := openDemoTestDB(t)

	provisioner := workspace.NewProvisioner()
	h := NewHandler(appPool, testVault(t), "http://127.0.0.1:8080", provisioner, nil, nil)

	rec := postDemoReset(t, h, userID, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}

func TestResetDemoRejectsMalformedBody(t *testing.T) {
	_, appPool, userID := openDemoTestDB(t)

	provisioner := workspace.NewProvisioner()
	starter := &fakeResetStarter{workflowID: "should-not-run"}
	h := NewHandler(appPool, testVault(t), "http://127.0.0.1:8080", provisioner, nil, starter)

	rec := postDemoReset(t, h, userID, "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if starter.calls != 0 {
		t.Fatalf("a malformed body must not start a workflow")
	}
}

// inlineResetStarter runs, synchronously, the same steps DemoResetWorkflow runs through its
// activities.
//
// The handler now only starts a workflow, so the end-to-end reset tests need something that
// actually rebuilds the sandbox for them to inspect. This is that stand-in. It calls the
// seeder from this package, which is the very function the worker injects into
// DemoActivities.SeedDemoSandboxActivity, so the tests still exercise real seeding.
type inlineResetStarter struct{ h *Handler }

func (i inlineResetStarter) Start(
	ctx context.Context,
	scope temporalinternal.SyncScope,
	_ bool,
) (string, error) {
	tx, err := db.BeginWorkspaceTx(ctx, i.h.pool, scope.UserID)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO user_preferences (user_id, demo_mode_enabled, updated_at)
		VALUES ($1, TRUE, NOW())
		ON CONFLICT (user_id) DO UPDATE SET demo_mode_enabled = TRUE, updated_at = NOW()
	`, scope.UserID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM workspaces
		WHERE id IN (
			SELECT w.id FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE wm.user_id = $1 AND w.is_demo = TRUE
		)
	`, scope.UserID); err != nil {
		return "", err
	}
	if _, err := SeedDemoWorkspaceTxWithID(ctx, tx, scope.UserID, uuid.New(), i.h.mockBaseURL, i.h.vault); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return "demo-reset-inline-" + scope.WorkspaceID.String(), nil
}

// settingsAfterReset reads the settings the reset workflow would have produced, since the
// endpoint itself now answers 202 with only a workflow id.
func settingsAfterReset(t *testing.T, h *Handler, userID uuid.UUID) *SettingsResponse {
	t.Helper()
	settings, err := h.loadSettingsResponse(context.Background(), userID)
	if err != nil {
		t.Fatalf("failed to load settings after reset: %v", err)
	}
	return settings
}
