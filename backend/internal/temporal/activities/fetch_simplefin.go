package activities

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

type BankActivities struct {
	Pool                  *pgxpool.Pool
	Client                simplefin.Client
	Vault                 crypto.Vault
	NotificationEvaluator notifications.Evaluator
	// MockBaseURL is the demo SimpleFIN bridge address this process dials, from
	// MOCK_SIMPLEFIN_BASE_URL. It rebases stored demo access URLs; see rebaseMockBridgeURL.
	MockBaseURL string
}

// mockBridgePathPrefix is the route the demo SimpleFIN bridge serves, registered in
// internal/demo/routes.go. A stored access URL carrying it points at our own bridge
// rather than a real bank, so its authority is a deployment detail and not a credential.
const mockBridgePathPrefix = "/mock-simplefin/"

// rebaseMockBridgeURL points a stored demo access URL at the bridge this process is
// configured to dial.
//
// The demo seeder bakes the bridge address into encrypted_access_url once, when the
// sandbox is built. Everything stored after that is a snapshot of wherever the API
// happened to serve the bridge: a renamed release, a moved namespace, or a sandbox
// seeded before the sync worker became its own Deployment all leave a URL that no
// longer resolves, and every sync then fails dialing the worker's own loopback while
// the chart's render-time guard stays happy, because the value it checks is the
// worker's environment and not the row. The bridge authorizes on the workspace ID in
// the path and the credential pair, so keep those and take only the scheme and
// authority from the configured base.
//
// Real SimpleFIN connections are returned untouched: their authority is the bank.
func rebaseMockBridgeURL(accessURL, configuredBase string) string {
	base := strings.TrimSpace(configuredBase)
	if base == "" {
		return accessURL
	}
	parsed, err := url.Parse(accessURL)
	if err != nil || !strings.HasPrefix(parsed.Path, mockBridgePathPrefix) {
		return accessURL
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Host == "" {
		return accessURL
	}
	if parsed.Scheme == baseURL.Scheme && parsed.Host == baseURL.Host {
		return accessURL
	}
	parsed.Scheme = baseURL.Scheme
	parsed.Host = baseURL.Host
	if basePath := strings.TrimRight(baseURL.Path, "/"); basePath != "" && !strings.HasPrefix(parsed.Path, basePath+"/") {
		parsed.Path = basePath + parsed.Path
	}
	return parsed.String()
}

func (a *BankActivities) FetchSimpleFINDataActivity(ctx context.Context, scope temporalinternal.SyncScope) (*simplefin.DataResponse, error) {
	var encryptedAccessURL string
	var lastSyncedAt *time.Time

	// Tenant scoping is mandatory here, not an optimisation: simplefin_connections
	// carries FORCE ROW LEVEL SECURITY, so an unscoped connection reads zero rows and
	// the workflow would finish "successfully" having fetched nothing. The fetch itself
	// stays outside the transaction so a slow bank never holds a pooled connection open.
	if err := db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(encrypted_access_url, ''), last_synced_at
			FROM simplefin_connections
			WHERE workspace_id = $1
		`, scope.WorkspaceID).Scan(&encryptedAccessURL, &lastSyncedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}); err != nil {
		return nil, err
	}

	if encryptedAccessURL == "" {
		return &simplefin.DataResponse{}, nil
	}

	accessURL, err := a.Vault.Decrypt(encryptedAccessURL)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(err.Error(), "SimpleFINDecryptError", err)
	}
	accessURL = rebaseMockBridgeURL(accessURL, a.MockBaseURL)

	since := time.Now().UTC().AddDate(0, 0, -30)
	if lastSyncedAt != nil {
		since = lastSyncedAt.Add(-7 * 24 * time.Hour)
	}

	data, err := a.Client.FetchData(ctx, accessURL, since)
	if err != nil {
		var authErr *simplefin.AuthError
		if errors.As(err, &authErr) || errors.Is(err, simplefin.ErrInvalidAccessURL) {
			return nil, temporal.NewNonRetryableApplicationError(err.Error(), "SimpleFINAuthError", err)
		}
		return nil, err
	}
	return data, nil
}
