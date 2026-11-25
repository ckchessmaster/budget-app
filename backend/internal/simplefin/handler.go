package simplefin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// ClaimWorkflow starts the durable SimpleFIN claim for a workspace and returns its
// workflow id.
//
// It is an interface rather than a direct call into the Temporal packages because the
// claim activities import this package for its client; importing them back would close a
// cycle. The server wires the concrete starter in.
type ClaimWorkflow interface {
	Start(ctx context.Context, scope temporalinternal.SyncScope) (string, error)
}

type ClaimTokenRequest struct {
	SetupToken string `json:"setup_token"`
}

type LinkAccountMapping struct {
	ExternalAccountID string `json:"external_account_id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	CardMode          string `json:"card_mode,omitempty"`
	Balance           string `json:"balance,omitempty"`
	IsOnBudget        *bool  `json:"is_on_budget,omitempty"`
}

type LinkAccountsRequest struct {
	Accounts []LinkAccountMapping `json:"accounts"`
}

// Handler owns the SimpleFIN HTTP surface. It deliberately holds no simplefin.Client:
// nothing in a request path may dial a bank. The only outbound work here is starting a
// workflow.
type Handler struct {
	pool  *pgxpool.Pool
	vault crypto.Vault
	claim ClaimWorkflow
}

func NewHandler(pool *pgxpool.Pool, vault crypto.Vault, claim ClaimWorkflow) *Handler {
	return &Handler{
		pool:  pool,
		vault: vault,
		claim: claim,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ClaimToken stages a SimpleFIN setup token and starts the workflow that exchanges it.
//
// It used to claim the token and fetch the account list inline, against a 30-second HTTP
// client. That meant the browser held a request open for as long as the bank took, a
// failure after a successful claim left a spent token with nothing retrying it, and a 429
// was handed straight to the user to click again. Now this returns 202 with a workflow id
// and the caller polls the shared status endpoint, so the bank can take as long as it
// needs and the retry happens with the bank's own Retry-After.
func (h *Handler) ClaimToken(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ClaimTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SetupToken) == "" {
		writeErr(w, http.StatusBadRequest, "setup_token is required")
		return
	}
	if h.claim == nil {
		slog.Error("simplefin claim requested but no workflow starter is configured")
		writeErr(w, http.StatusServiceUnavailable, "temporal_unavailable")
		return
	}

	var wsID uuid.UUID
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		wsID, txErr = db.ActiveWorkspaceID(r.Context(), tx)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "workspace_not_found")
		return
	}

	// The setup token never travels as a workflow argument: Temporal records inputs in
	// history, which operators can read and which outlives the token. It is sealed here
	// and picked up by the worker from this row instead.
	sealed, err := h.vault.Encrypt(strings.TrimSpace(req.SetupToken))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_encrypt_credentials")
		return
	}

	// Staging leaves encrypted_access_url alone on purpose. A claim that never completes
	// therefore costs nothing: the workspace keeps whatever connection it already had,
	// and the demo status check still reports it as disconnected until an access URL lands.
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, pending_claim_token)
			VALUES ($1, $2, $2)
			ON CONFLICT (workspace_id) DO UPDATE
			SET pending_claim_token = EXCLUDED.pending_claim_token
		`, wsID, sealed)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_stage_connection")
		return
	}

	workflowID, err := h.claim.Start(r.Context(), temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID})
	if err != nil {
		slog.Error("failed to start simplefin claim workflow",
			"workspace_id", wsID, "error", err)
		// The staged token is inert, but leaving it behind would make the next claim look
		// like a retry of this one. Best-effort cleanup on a context the request cannot
		// cancel.
		clearCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if clearErr := db.WithWorkspaceTx(clearCtx, h.pool, userID, func(tx pgx.Tx) error {
			_, err := tx.Exec(clearCtx, `
				UPDATE simplefin_connections SET pending_claim_token = NULL
				WHERE workspace_id = $1
			`, wsID)
			return err
		}); clearErr != nil {
			slog.Warn("failed to clear staged simplefin claim token",
				"workspace_id", wsID, "error", clearErr)
		}
		writeErr(w, http.StatusBadGateway, "failed_to_start_claim")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"workflow_id": workflowID,
		"status":      "accepted",
	})
}

func (h *Handler) LinkAccounts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req LinkAccountsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Accounts) == 0 {
		writeErr(w, http.StatusBadRequest, "accounts list is required")
		return
	}

	var linked []account.Account
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		wsID, err := db.ActiveWorkspaceID(r.Context(), tx)
		if err != nil {
			return err
		}

		for _, m := range req.Accounts {
			accType := account.AccountType(m.Type)
			switch accType {
			case account.Checking, account.Savings, account.Cash, account.CreditCard, account.Investment, account.Loan:
			default:
				return account.ErrInvalidAccountType
			}

			cardMode := account.CCMode(m.CardMode)
			if cardMode == "" {
				cardMode = account.PayInFull
			}

			isOnBudget := true
			if m.IsOnBudget != nil {
				isOnBudget = *m.IsOnBudget
			} else if accType == account.Investment || accType == account.Loan {
				isOnBudget = false
			}

			bal := decimal.Zero
			if m.Balance != "" {
				if parsed, err := decimal.NewFromString(m.Balance); err == nil {
					bal = parsed.Round(2)
				}
			}

			var existingID uuid.UUID
			err := tx.QueryRow(r.Context(), `
				SELECT id FROM accounts WHERE workspace_id = $1 AND external_account_id = $2
			`, wsID, m.ExternalAccountID).Scan(&existingID)

			if errors.Is(err, pgx.ErrNoRows) {
				var acct account.Account
				extID := m.ExternalAccountID
				acct = account.Account{
					ID:                uuid.New(),
					WorkspaceID:       wsID,
					ExternalAccountID: &extID,
					Name:              m.Name,
					Type:              accType,
					CurrentBalance:    bal,
					IsOnBudget:        isOnBudget,
					CardMode:          cardMode,
				}
				if err := tx.QueryRow(r.Context(), `
					INSERT INTO accounts (id, workspace_id, external_account_id, name, type, current_balance, is_on_budget, card_mode)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
					RETURNING created_at, updated_at
				`, acct.ID, acct.WorkspaceID, acct.ExternalAccountID, acct.Name, string(acct.Type),
					acct.CurrentBalance, acct.IsOnBudget, string(acct.CardMode),
				).Scan(&acct.CreatedAt, &acct.UpdatedAt); err != nil {
					return err
				}
				linked = append(linked, acct)
			} else if err != nil {
				return err
			} else {
				var acct account.Account
				extID := m.ExternalAccountID
				acct = account.Account{
					ID:                existingID,
					WorkspaceID:       wsID,
					ExternalAccountID: &extID,
					Name:              m.Name,
					Type:              accType,
					CurrentBalance:    bal,
					IsOnBudget:        isOnBudget,
					CardMode:          cardMode,
				}
				if err := tx.QueryRow(r.Context(), `
					UPDATE accounts
					SET name = $2, type = $3, is_on_budget = $4, card_mode = $5, updated_at = NOW()
					WHERE id = $1
					RETURNING current_balance, created_at, updated_at
				`, existingID, m.Name, string(accType), isOnBudget, string(cardMode)).Scan(
					&acct.CurrentBalance, &acct.CreatedAt, &acct.UpdatedAt,
				); err != nil {
					return err
				}
				linked = append(linked, acct)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, account.ErrInvalidAccountType) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_link_accounts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"accounts": linked})
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	unlinkAccounts := r.URL.Query().Get("unlink_accounts") == "true"

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		wsID, err := db.ActiveWorkspaceID(r.Context(), tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			DELETE FROM simplefin_connections WHERE workspace_id = $1
		`, wsID); err != nil {
			return err
		}
		if unlinkAccounts {
			if _, err := tx.Exec(r.Context(), `
				UPDATE accounts SET external_account_id = NULL, updated_at = NOW() WHERE workspace_id = $1
			`, wsID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_disconnect_simplefin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
