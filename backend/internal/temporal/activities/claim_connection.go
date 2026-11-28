package activities

import (
	"context"
	"errors"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

// ClaimActivities is the SimpleFIN claim sequence that used to run inside the
// POST /api/v1/bank/simplefin/claim request. It lives here so a slow, rate-limited or
// erroring bank cannot hold a browser request open, and so a claim that succeeds but then
// fails to finish is retried instead of leaving a spent token behind.
//
// No credential is an argument to any activity in here. The API stages the sealed setup
// token in simplefin_connections.pending_claim_token and starts the workflow with a scope
// alone, because Temporal records activity inputs in history, which operators can read.
type ClaimActivities struct {
	Pool   *pgxpool.Pool
	Client simplefin.Client
	Vault  crypto.Vault
}

// ClaimConnectionResult reports the connection the claim produced.
// simplefin_connections is keyed by workspace, so ConnectionID is the workspace ID rather
// than a separate row identity.
type ClaimConnectionResult struct {
	ConnectionID string `json:"connection_id"`
	Reused       bool   `json:"reused,omitempty"`
}

// DiscoveredAccount is the safe, non-credential projection of a bank account used to
// render the linking step. It is deliberately not simplefin.AccountDTO, so a field added
// to that type later cannot leak into Temporal history by accident.
type DiscoveredAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Balance  string `json:"balance"`
}

// FetchDiscoveredAccountsResult carries the accounts plus anything the bank reported as a
// non-fatal error alongside them.
type FetchDiscoveredAccountsResult struct {
	Accounts []DiscoveredAccount `json:"accounts"`
	Warnings []string            `json:"warnings,omitempty"`
}

// ClassifySimpleFINError maps a client error onto the failure the retry policy should see.
//
// A rate limit carries the bank's own Retry-After, which is honoured through
// NextRetryDelay rather than the generic backoff — that header is the whole reason the
// client models 429 separately. A rejected or spent token is terminal: resending it can
// never succeed, it only burns attempts against a credential that is already gone.
// Everything else — 5xx, transport, decode — is retryable.
func ClassifySimpleFINError(err error) error {
	if err == nil {
		return nil
	}

	var rateLimited *simplefin.RateLimitError
	if errors.As(err, &rateLimited) {
		return temporal.NewApplicationErrorWithOptions(
			"simplefin rate limited",
			"SimpleFINRateLimited",
			temporal.ApplicationErrorOptions{Cause: err, NextRetryDelay: rateLimited.RetryAfter},
		)
	}

	var authErr *simplefin.AuthError
	var terminal *simplefin.TerminalStatusError
	if errors.As(err, &terminal) ||
		errors.As(err, &authErr) ||
		errors.Is(err, simplefin.ErrInvalidSetupToken) ||
		errors.Is(err, simplefin.ErrInvalidAccessURL) {
		return temporal.NewNonRetryableApplicationError("simplefin rejected the credentials", "SimpleFINTokenRejected", err)
	}

	return err
}

// ClaimConnectionActivity exchanges the staged setup token for an access URL and stores it.
//
// It is safe to retry. A claim token is single-use at the bank, so the staged token is
// cleared in the same statement that writes the access URL: a late retry finds nothing
// pending and reports success rather than spending a token that is already spent. A
// failure leaves both the staged token and any previously working access URL in place.
func (a *ClaimActivities) ClaimConnectionActivity(
	ctx context.Context,
	scope temporalinternal.SyncScope,
) (ClaimConnectionResult, error) {
	if scope.WorkspaceID == uuid.Nil || scope.UserID == uuid.Nil {
		return ClaimConnectionResult{}, temporal.NewNonRetryableApplicationError(
			"workspace_id and user_id are required to claim a SimpleFIN connection",
			"InvalidClaimScope", nil)
	}
	ctx = db.WithWorkspaceID(ctx, scope.WorkspaceID)

	var pending string
	err := db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(pending_claim_token, '')
			FROM simplefin_connections
			WHERE workspace_id = $1
		`, scope.WorkspaceID).Scan(&pending)
		if errors.Is(err, pgx.ErrNoRows) {
			return temporal.NewNonRetryableApplicationError(
				"no SimpleFIN claim is pending for this workspace", "SimpleFINClaimNotFound", nil)
		}
		return err
	})
	if err != nil {
		return ClaimConnectionResult{}, err
	}

	// Nothing staged means an earlier attempt already completed the claim and only its
	// confirmation was lost.
	if pending == "" {
		return ClaimConnectionResult{ConnectionID: scope.WorkspaceID.String(), Reused: true}, nil
	}

	setupToken, err := a.Vault.Decrypt(pending)
	if err != nil {
		return ClaimConnectionResult{}, temporal.NewNonRetryableApplicationError(
			"failed to open the staged SimpleFIN setup token", "SimpleFINDecryptError", err)
	}

	accessURL, err := a.Client.ClaimSetupToken(ctx, setupToken)
	if err != nil {
		return ClaimConnectionResult{}, ClassifySimpleFINError(err)
	}

	sealed, err := a.Vault.Encrypt(accessURL)
	if err != nil {
		return ClaimConnectionResult{}, temporal.NewNonRetryableApplicationError(
			"failed to seal SimpleFIN credentials", "VaultSealFailed", err)
	}

	err = db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE simplefin_connections
			SET encrypted_access_url = $2,
			    pending_claim_token = NULL
			WHERE workspace_id = $1
		`, scope.WorkspaceID, sealed)
		return err
	})
	if err != nil {
		return ClaimConnectionResult{}, err
	}

	return ClaimConnectionResult{ConnectionID: scope.WorkspaceID.String()}, nil
}

// FetchDiscoveredAccountsActivity reads back the connection the claim stored and lists the
// accounts behind it. Keeping it separate from the claim is what makes the claim safe to
// retry: a fetch failure never re-dials the claim endpoint.
func (a *ClaimActivities) FetchDiscoveredAccountsActivity(
	ctx context.Context,
	scope temporalinternal.SyncScope,
) (*FetchDiscoveredAccountsResult, error) {
	ctx = db.WithWorkspaceID(ctx, scope.WorkspaceID)
	var encryptedAccessURL string
	err := db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(encrypted_access_url, '')
			FROM simplefin_connections
			WHERE workspace_id = $1
		`, scope.WorkspaceID).Scan(&encryptedAccessURL)
		if errors.Is(err, pgx.ErrNoRows) {
			return temporal.NewNonRetryableApplicationError(
				"no SimpleFIN connection is linked to this workspace", "SimpleFINConnectionMissing", nil)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if encryptedAccessURL == "" {
		return nil, temporal.NewNonRetryableApplicationError(
			"the stored SimpleFIN connection has no access URL", "SimpleFINConnectionIncomplete", nil)
	}

	accessURL, err := a.Vault.Decrypt(encryptedAccessURL)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"failed to open the stored SimpleFIN credentials", "SimpleFINDecryptError", err)
	}

	data, err := a.Client.FetchData(ctx, accessURL, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		return nil, ClassifySimpleFINError(err)
	}

	out := &FetchDiscoveredAccountsResult{
		Accounts: make([]DiscoveredAccount, 0, len(data.Accounts)),
		Warnings: append([]string(nil), data.Errors...),
	}
	for _, acct := range data.Accounts {
		out.Accounts = append(out.Accounts, DiscoveredAccount{
			ID:       acct.ID,
			Name:     acct.Name,
			Currency: acct.Currency,
			Balance:  acct.Balance,
		})
	}
	return out, nil
}
