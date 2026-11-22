package activities

import (
	"context"
	"errors"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

type BankActivities struct {
	Pool   *pgxpool.Pool
	Client simplefin.Client
	Vault  crypto.Vault
}

func (a *BankActivities) FetchSimpleFINDataActivity(ctx context.Context, workspaceID uuid.UUID) (*simplefin.DataResponse, error) {
	var encryptedAccessURL string
	var lastSyncedAt *time.Time

	err := a.Pool.QueryRow(ctx, `
		SELECT encrypted_access_url, last_synced_at
		FROM simplefin_connections
		WHERE workspace_id = $1
	`, workspaceID).Scan(&encryptedAccessURL, &lastSyncedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &simplefin.DataResponse{}, nil
		}
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
