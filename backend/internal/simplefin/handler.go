package simplefin

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

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

type Handler struct {
	pool   *pgxpool.Pool
	client Client
	vault  crypto.Vault
}

func NewHandler(pool *pgxpool.Pool, client Client, vault crypto.Vault) *Handler {
	return &Handler{
		pool:   pool,
		client: client,
		vault:  vault,
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

func (h *Handler) ClaimToken(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ClaimTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SetupToken == "" {
		writeErr(w, http.StatusBadRequest, "setup_token is required")
		return
	}

	accessURL, err := h.client.ClaimSetupToken(r.Context(), req.SetupToken)
	if err != nil {
		if errors.Is(err, ErrInvalidSetupToken) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		var rlErr *RateLimitError
		if errors.As(err, &rlErr) {
			writeErr(w, http.StatusTooManyRequests, rlErr.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, "failed_to_claim_simplefin_token")
		return
	}

	encryptedAccessURL, err := h.vault.Encrypt(accessURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_encrypt_credentials")
		return
	}

	encryptedClaimToken, err := h.vault.Encrypt(req.SetupToken)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_encrypt_credentials")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var wsID uuid.UUID
		if err := tx.QueryRow(r.Context(), `SELECT workspace_id FROM workspace_members LIMIT 1`).Scan(&wsID); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, encrypted_access_url)
			VALUES ($1, $2, $3)
			ON CONFLICT (workspace_id) DO UPDATE
			SET encrypted_claim_token = EXCLUDED.encrypted_claim_token,
			    encrypted_access_url = EXCLUDED.encrypted_access_url
		`, wsID, encryptedClaimToken, encryptedAccessURL)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_store_connection")
		return
	}

	data, err := h.client.FetchData(r.Context(), accessURL, time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "failed_to_fetch_simplefin_accounts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": data.Accounts,
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
		var wsID uuid.UUID
		if err := tx.QueryRow(r.Context(), `SELECT workspace_id FROM workspace_members LIMIT 1`).Scan(&wsID); err != nil {
			return err
		}

		for _, m := range req.Accounts {
			accType := account.AccountType(m.Type)
			switch accType {
			case account.Checking, account.Savings, account.CreditCard, account.Investment, account.Loan:
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
