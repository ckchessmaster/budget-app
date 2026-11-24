package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// openDemoTestDB connects with the production role split: DATABASE_URL is a superuser used
// for fixture setup and teardown, TEST_DATABASE_URL is the unprivileged application role
// that every handler runs as, so FORCE ROW LEVEL SECURITY really applies.
func openDemoTestDB(t *testing.T) (adminPool *pgxpool.Pool, appPool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// DATABASE_URL is the privileged role, TEST_DATABASE_URL the application role.
	adminURL := os.Getenv("DATABASE_URL")
	if adminURL == "" {
		adminURL = os.Getenv("TEST_DATABASE_URL")
	}
	if adminURL == "" {
		adminURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}
	appURL := os.Getenv("TEST_DATABASE_URL")
	if appURL == "" {
		appURL = adminURL
	}

	adminPool, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Skipf("skipping DB integration test (admin pool error): %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping DB integration test (postgres unreachable): %v", err)
	}

	appPool, err = pgxpool.New(ctx, appURL)
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping DB integration test (app pool error): %v", err)
	}

	userID = uuid.New()
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM user_preferences WHERE user_id = $1", userID)
		_, _ = adminPool.Exec(bg, `
			DELETE FROM workspaces WHERE id IN (
				SELECT workspace_id FROM workspace_members WHERE user_id = $1
			)
		`, userID)
		_, _ = adminPool.Exec(bg, "DELETE FROM workspace_members WHERE user_id = $1", userID)
		adminPool.Close()
		appPool.Close()
	})

	return adminPool, appPool, userID
}

type toggleSettings struct {
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	WorkspaceName   string    `json:"workspace_name"`
	IsDemo          bool      `json:"is_demo"`
	DemoModeEnabled bool      `json:"demo_mode_enabled"`
}

func postToggle(t *testing.T, h *Handler, userID uuid.UUID, enabled bool) (int, toggleSettings) {
	t.Helper()

	body := []byte(`{"enabled":true}`)
	if !enabled {
		body = []byte(`{"enabled":false}`)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.ToggleDemo(rec, req)

	var settings toggleSettings
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
			t.Fatalf("toggle returned 200 but body was not settings JSON: %v (%s)", err, rec.Body.String())
		}
	}
	return rec.Code, settings
}

// TestToggleDemoSeedsSandboxUnderRLS covers enabling demo mode end to end through the
// handler. Toggling drives several statements in one hand-managed transaction, which the
// RLS context must wrap: an unscoped transaction sees no tenant rows and its inserts are
// rejected with "new row violates row-level security policy". The preference also has to be
// written before seeding, because the workspace policies hide a demo workspace until
// demo_mode_enabled is true, and the seed writes are only visible against a workspace the
// caller can already see.
func TestToggleDemoSeedsSandboxUnderRLS(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	provisioner := workspace.NewProvisioner()
	h := NewHandler(appPool, testVault(t), "http://127.0.0.1:8080", provisioner, nil)

	if _, err := provisioner.EnsureWorkspaceProvisioned(ctx, appPool, userID); err != nil {
		t.Fatalf("failed to provision real workspace: %v", err)
	}

	status, settings := postToggle(t, h, userID, true)
	if status != http.StatusOK {
		t.Fatalf("toggle on status = %d, want 200", status)
	}
	if !settings.DemoModeEnabled || !settings.IsDemo {
		t.Fatalf("toggle on settings = %+v, want demo mode enabled and an active demo workspace", settings)
	}

	demoID := settings.WorkspaceID

	var demoCount int
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.user_id = $1 AND w.is_demo = TRUE
	`, userID).Scan(&demoCount); err != nil {
		t.Fatalf("failed to count demo workspaces: %v", err)
	}
	if demoCount != 1 {
		t.Fatalf("demo workspaces = %d, want exactly 1", demoCount)
	}

	// The sandbox must actually be seeded, and reachable through the same RLS-scoped
	// resolution every read endpoint uses.
	var categories, accounts int
	err := db.WithWorkspaceTx(ctx, appPool, userID, func(tx pgx.Tx) error {
		activeID, err := db.ActiveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		if activeID != settings.WorkspaceID {
			t.Errorf("active workspace = %s, want the demo workspace %s", activeID, settings.WorkspaceID)
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM categories WHERE workspace_id = $1`, activeID).Scan(&categories); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE workspace_id = $1`, activeID).Scan(&accounts); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeded sandbox was not readable through the workspace transaction: %v", err)
	}
	if categories == 0 || accounts == 0 {
		t.Fatalf("seeded sandbox incomplete: categories = %d, accounts = %d, want both non-zero", categories, accounts)
	}

	// Turning demo off returns the user to their real workspace.
	status, settings = postToggle(t, h, userID, false)
	if status != http.StatusOK {
		t.Fatalf("toggle off status = %d, want 200", status)
	}
	if settings.DemoModeEnabled {
		t.Fatalf("toggle off settings = %+v, want demo mode disabled", settings)
	}

	// Turning it back on must reuse the existing sandbox rather than seeding a second one,
	// which only works if the seed path can see a sandbox the preference already exposes.
	status, second := postToggle(t, h, userID, true)
	if status != http.StatusOK {
		t.Fatalf("second toggle on status = %d, want 200", status)
	}
	if !second.IsDemo || !second.DemoModeEnabled {
		t.Fatalf("second toggle settings = %+v, want demo mode enabled on a demo workspace", second)
	}
	if second.WorkspaceID != demoID {
		t.Fatalf("second toggle resolved workspace %s, want the existing sandbox %s", second.WorkspaceID, demoID)
	}
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.user_id = $1 AND w.is_demo = TRUE
	`, userID).Scan(&demoCount); err != nil {
		t.Fatalf("failed to recount demo workspaces: %v", err)
	}
	if demoCount != 1 {
		t.Fatalf("demo workspaces after re-toggle = %d, want still exactly 1", demoCount)
	}
}

// TestResetDemoRebuildsSandboxUnderRLS covers the reset path, which deletes the existing
// sandbox and seeds a replacement in one hand-managed transaction.
func TestResetDemoRebuildsSandboxUnderRLS(t *testing.T) {
	adminPool, appPool, userID := openDemoTestDB(t)
	ctx := context.Background()

	provisioner := workspace.NewProvisioner()
	h := NewHandler(appPool, testVault(t), "http://127.0.0.1:8080", provisioner, nil)

	if _, err := provisioner.EnsureWorkspaceProvisioned(ctx, appPool, userID); err != nil {
		t.Fatalf("failed to provision real workspace: %v", err)
	}

	status, first := postToggle(t, h, userID, true)
	if status != http.StatusOK {
		t.Fatalf("toggle on status = %d, want 200", status)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/reset", nil)
	ctx2 := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	rec := httptest.NewRecorder()
	h.ResetDemo(rec, req.WithContext(ctx2))
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var resetSettings toggleSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &resetSettings); err != nil {
		t.Fatalf("reset body was not settings JSON: %v (%s)", err, rec.Body.String())
	}
	if resetSettings.WorkspaceID == first.WorkspaceID {
		t.Fatalf("reset kept workspace %s, want a freshly seeded sandbox", resetSettings.WorkspaceID)
	}

	var demoCount int
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.user_id = $1 AND w.is_demo = TRUE
	`, userID).Scan(&demoCount); err != nil {
		t.Fatalf("failed to count demo workspaces after reset: %v", err)
	}
	if demoCount != 1 {
		t.Fatalf("demo workspaces after reset = %d, want exactly 1 (the old sandbox should be gone)", demoCount)
	}

	var categories int
	if err := adminPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM categories WHERE workspace_id = $1
	`, resetSettings.WorkspaceID).Scan(&categories); err != nil {
		t.Fatalf("failed to count categories in the reset sandbox: %v", err)
	}
	if categories == 0 {
		t.Fatal("reset sandbox has no seeded categories")
	}
}
