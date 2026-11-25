package activities

import (
	"context"
	"errors"
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
