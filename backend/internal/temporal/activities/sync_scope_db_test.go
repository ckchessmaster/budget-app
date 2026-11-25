package activities

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

// recordingClient reports whether the sync actually reached the bank, which is what makes
// the RLS scoping observable: an unscoped activity reads no connection row and therefore
// never calls out, while still returning a response the workflow reports as success.
type recordingClient struct {
	calls []string
	data  *simplefin.DataResponse
}

func (c *recordingClient) ClaimSetupToken(context.Context, string) (string, error) {
	return "", nil
}

func (c *recordingClient) FetchData(_ context.Context, accessURL string, _ time.Time) (*simplefin.DataResponse, error) {
	c.calls = append(c.calls, accessURL)
	if c.data == nil {
		return &simplefin.DataResponse{}, nil
	}
	return c.data, nil
}

func openScopeTestDB(t *testing.T) (adminPool *pgxpool.Pool, appPool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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

	t.Cleanup(func() {
		adminPool.Close()
		appPool.Close()
	})

	return adminPool, appPool
}

// provisionWorkspace creates a fresh user with one workspace and removes everything it
// seeded when the test ends, including the connection row this suite writes directly as
// the privileged role.
func provisionWorkspace(t *testing.T, adminPool, appPool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	prov := workspace.NewProvisioner()
	res, err := prov.EnsureWorkspaceProvisioned(context.Background(), appPool, userID)
	if err != nil {
		t.Fatalf("failed to provision workspace: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		_, _ = adminPool.Exec(bg, "DELETE FROM simplefin_connections WHERE workspace_id = $1", res.WorkspaceID)
		_, _ = adminPool.Exec(bg, "DELETE FROM user_preferences WHERE user_id = $1", userID)
		_, _ = adminPool.Exec(bg, "DELETE FROM workspaces WHERE id = $1", res.WorkspaceID)
		_, _ = adminPool.Exec(bg, "DELETE FROM workspace_members WHERE user_id = $1", userID)
	})

	return userID, res.WorkspaceID
}

func TestFetchSimpleFINDataActivityIsScopedToTheSyncUser(t *testing.T) {
	adminPool, appPool := openScopeTestDB(t)
	ctx := context.Background()

	ownerID, ownerWS := provisionWorkspace(t, adminPool, appPool)
	intruderID, _ := provisionWorkspace(t, adminPool, appPool)

	vault, err := crypto.NewAESGCMVault(make([]byte, crypto.AES256KeySize))
	if err != nil {
		t.Fatalf("failed to build vault: %v", err)
	}
	const wantURL = "http://127.0.0.1:8090/mock-simplefin/anything/accounts"
	sealed, err := vault.Encrypt(wantURL)
	if err != nil {
		t.Fatalf("failed to seal access url: %v", err)
	}
	sealedClaim, err := vault.Encrypt("mock-claim-token")
	if err != nil {
		t.Fatalf("failed to seal claim token: %v", err)
	}

	// The privileged role seeds the row; the application role below must still be able to
	// see it only through its own row-level security context.
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, encrypted_access_url)
		VALUES ($1, $2, $3)
	`, ownerWS, sealedClaim, sealed); err != nil {
		t.Fatalf("failed to seed connection: %v", err)
	}

	t.Run("the owning identity reaches the bank", func(t *testing.T) {
		client := &recordingClient{}
		activities := &BankActivities{Pool: appPool, Client: client, Vault: vault}

		res, err := activities.FetchSimpleFINDataActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
			UserID:      ownerID,
		})
		if err != nil {
			t.Fatalf("fetch activity returned an error: %v", err)
		}
		if res == nil {
			t.Fatal("expected a response")
		}
		if len(client.calls) != 1 || client.calls[0] != wantURL {
			t.Fatalf("expected exactly one fetch of %q, got %v", wantURL, client.calls)
		}
	})

	// The row above is the shape a demo sandbox has after the sync worker moved into its
	// own Deployment: the loopback bridge address is what got stored, and the address the
	// worker can actually dial is configuration. Syncing that sandbox has to use the
	// latter without waiting for someone to re-seed it.
	t.Run("a demo row seeded with a stale bridge address dials the configured one", func(t *testing.T) {
		client := &recordingClient{}
		activities := &BankActivities{
			Pool:        appPool,
			Client:      client,
			Vault:       vault,
			MockBaseURL: "http://budget-app-budget-app-backend-bridge:8090",
		}

		if _, err := activities.FetchSimpleFINDataActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
			UserID:      ownerID,
		}); err != nil {
			t.Fatalf("fetch activity returned an error: %v", err)
		}

		want := "http://budget-app-budget-app-backend-bridge:8090/mock-simplefin/anything/accounts"
		if len(client.calls) != 1 || client.calls[0] != want {
			t.Fatalf("expected exactly one fetch of %q, got %v", want, client.calls)
		}
	})

	t.Run("another tenant cannot read the stored access url", func(t *testing.T) {
		client := &recordingClient{}
		activities := &BankActivities{Pool: appPool, Client: client, Vault: vault}

		if _, err := activities.FetchSimpleFINDataActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
			UserID:      intruderID,
		}); err != nil {
			t.Fatalf("fetch activity returned an error: %v", err)
		}
		if len(client.calls) != 0 {
			t.Fatalf("cross-tenant sync reached the bank with %v", client.calls)
		}
	})

	t.Run("an identity-less sync cannot read it either", func(t *testing.T) {
		client := &recordingClient{}
		activities := &BankActivities{Pool: appPool, Client: client, Vault: vault}

		if _, err := activities.FetchSimpleFINDataActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
		}); err != nil {
			t.Fatalf("fetch activity returned an error: %v", err)
		}
		if len(client.calls) != 0 {
			t.Fatalf("unscoped sync reached the bank with %v", client.calls)
		}
	})
}

type scopeRecordingPush struct {
	sent []notifications.NotificationPayload
}

func (p *scopeRecordingPush) Enabled() bool     { return true }
func (p *scopeRecordingPush) PublicKey() string { return "pub" }
func (p *scopeRecordingPush) Subscribe(context.Context, uuid.UUID, notifications.PushSubscriptionDTO) error {
	return nil
}
func (p *scopeRecordingPush) Unsubscribe(context.Context, uuid.UUID, string) error {
	return nil
}
func (p *scopeRecordingPush) SendNotification(_ context.Context, _ uuid.UUID, payload notifications.NotificationPayload) error {
	p.sent = append(p.sent, payload)
	return nil
}

func TestEvaluateNotificationsActivityIsScopedToTheSyncUser(t *testing.T) {
	adminPool, appPool := openScopeTestDB(t)
	ctx := context.Background()

	ownerID, ownerWS := provisionWorkspace(t, adminPool, appPool)
	intruderID, _ := provisionWorkspace(t, adminPool, appPool)

	now := time.Now().UTC()
	txDate := now.Format("2006-01-02")

	if err := db.WithWorkspaceTx(ctx, appPool, ownerID, func(tx pgx.Tx) error {
		var acctID, catID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
			VALUES ($1, 'Checking', 'checking', 1000.00, TRUE)
			RETURNING id
		`, ownerWS).Scan(&acctID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (workspace_id, name, group_name, monthly_budget, sort_order)
			VALUES ($1, 'Dining', 'Fun', 100.00, 1)
			RETURNING id
		`, ownerWS).Scan(&catID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions (workspace_id, account_id, category_id, origin, status, amount, merchant_raw, transaction_date, is_cleared)
			VALUES ($1, $2, $3, 'BANK_FEED', 'POSTED', -85.00, 'Bistro', $4::date, TRUE)
		`, ownerWS, acctID, catID, txDate)
		return err
	}); err != nil {
		t.Fatalf("failed to seed owner spending state: %v", err)
	}

	t.Run("the owning identity dispatches alert and cooldown suppresses repeat", func(t *testing.T) {
		push := &scopeRecordingPush{}
		eval := notifications.NewEvaluator(appPool, push).WithClock(func() time.Time { return now })
		acts := &BankActivities{Pool: appPool, NotificationEvaluator: eval}

		res1, err := acts.EvaluateNotificationsActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
			UserID:      ownerID,
		})
		if err != nil {
			t.Fatalf("EvaluateNotificationsActivity #1 error: %v", err)
		}
		if res1.SpendingAlertsSent != 1 || len(push.sent) != 1 {
			t.Fatalf("expected 1 alert sent on first pass, got res=%+v sent=%d", res1, len(push.sent))
		}

		res2, err := acts.EvaluateNotificationsActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
			UserID:      ownerID,
		})
		if err != nil {
			t.Fatalf("EvaluateNotificationsActivity #2 error: %v", err)
		}
		if res2.SpendingAlertsSent != 0 || res2.SuppressedByCooldown != 1 {
			t.Fatalf("expected cooldown suppression on repeat, got %+v", res2)
		}
	})

	t.Run("another tenant cannot trigger notifications for owner workspace", func(t *testing.T) {
		push := &scopeRecordingPush{}
		eval := notifications.NewEvaluator(appPool, push).WithClock(func() time.Time { return now.Add(25 * time.Hour) })
		acts := &BankActivities{Pool: appPool, NotificationEvaluator: eval}

		res, err := acts.EvaluateNotificationsActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
			UserID:      intruderID,
		})
		if err != nil {
			t.Fatalf("EvaluateNotificationsActivity intruder error: %v", err)
		}
		if res.TotalSent() != 0 || len(push.sent) != 0 {
			t.Fatalf("intruder dispatched alerts on owner workspace: res=%+v sent=%d", res, len(push.sent))
		}
	})

	t.Run("identity-less sync dispatches zero notifications", func(t *testing.T) {
		push := &scopeRecordingPush{}
		eval := notifications.NewEvaluator(appPool, push).WithClock(func() time.Time { return now.Add(25 * time.Hour) })
		acts := &BankActivities{Pool: appPool, NotificationEvaluator: eval}

		res, err := acts.EvaluateNotificationsActivity(ctx, temporalinternal.SyncScope{
			WorkspaceID: ownerWS,
		})
		if err != nil {
			t.Fatalf("EvaluateNotificationsActivity unscoped error: %v", err)
		}
		if res.TotalSent() != 0 || len(push.sent) != 0 {
			t.Fatalf("unscoped sync dispatched alerts: res=%+v sent=%d", res, len(push.sent))
		}
	})
}
