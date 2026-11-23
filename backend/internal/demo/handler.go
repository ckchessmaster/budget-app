package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/schedules"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type SimpleFINStatus struct {
	Connected        bool       `json:"connected"`
	IsDemoBridge     bool       `json:"is_demo_bridge"`
	LastSyncedAt     *time.Time `json:"last_synced_at"`
	LastSyncWarnings []string   `json:"last_sync_warnings"`
}

type SettingsResponse struct {
	WorkspaceID         uuid.UUID       `json:"workspace_id"`
	WorkspaceName       string          `json:"workspace_name"`
	IsDemo              bool            `json:"is_demo"`
	DemoModeEnabled     bool            `json:"demo_mode_enabled"`
	InboundReceiptAlias string          `json:"inbound_receipt_alias"`
	SimpleFIN           SimpleFINStatus `json:"simplefin"`
}

type SimAccount struct {
	ID                uuid.UUID `json:"id"`
	ExternalAccountID string    `json:"external_account_id"`
	Name              string    `json:"name"`
	Currency          string    `json:"currency"`
	Balance           string    `json:"balance"`
	AccountType       string    `json:"account_type"`
	CardMode          string    `json:"card_mode"`
}

type SimTransaction struct {
	ID                    uuid.UUID `json:"id"`
	ExternalAccountID     string    `json:"external_account_id"`
	ExternalTransactionID string    `json:"external_transaction_id"`
	PostedAt              time.Time `json:"posted_at"`
	Amount                string    `json:"amount"`
	Description           string    `json:"description"`
	Pending               bool      `json:"pending"`
	IsSynced              bool      `json:"is_synced"`
}

type SimManualTransaction struct {
	ID                uuid.UUID `json:"id"`
	AccountID         uuid.UUID `json:"account_id"`
	AccountName       string    `json:"account_name"`
	ExternalAccountID *string   `json:"external_account_id"`
	Amount            string    `json:"amount"`
	MerchantRaw       string    `json:"merchant_raw"`
	TransactionDate   string    `json:"transaction_date"`
	Status            string    `json:"status"`
}

type SimFaultState struct {
	Mode              string `json:"mode"`
	Persistent        bool   `json:"persistent"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	Message           string `json:"message"`
}

type SimulatorStateResponse struct {
	Accounts           []SimAccount           `json:"accounts"`
	Transactions       []SimTransaction       `json:"transactions"`
	ManualTransactions []SimManualTransaction `json:"manual_transactions"`
	Fault              SimFaultState          `json:"fault"`
	UnsyncedCount      int                    `json:"unsynced_count"`
}

type Handler struct {
	pool        *pgxpool.Pool
	vault       crypto.Vault
	mockBaseURL string
	provisioner workspace.Provisioner
}

func NewHandler(
	pool *pgxpool.Pool,
	vault crypto.Vault,
	mockBaseURL string,
	provisioner workspace.Provisioner,
) *Handler {
	return &Handler{
		pool:        pool,
		vault:       vault,
		mockBaseURL: mockBaseURL,
		provisioner: provisioner,
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

var errDemoModeNotActive = errors.New("demo mode is not active")

func (h *Handler) resolveActiveDemoWorkspace(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	var wsID uuid.UUID
	var isDemo bool
	err := db.WithWorkspaceTx(ctx, h.pool, userID, func(tx pgx.Tx) error {
		id, err := db.ActiveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT is_demo FROM workspaces WHERE id = $1`, id).Scan(&isDemo); err != nil {
			return err
		}
		wsID = id
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	if !isDemo {
		return uuid.Nil, errDemoModeNotActive
	}
	return wsID, nil
}

func (h *Handler) loadSettingsResponse(ctx context.Context, userID uuid.UUID) (*SettingsResponse, error) {
	prov, err := h.provisioner.EnsureWorkspaceProvisioned(ctx, h.pool, userID)
	if err != nil {
		return nil, err
	}

	sf := SimpleFINStatus{
		LastSyncWarnings: []string{},
	}

	err = db.WithWorkspaceTx(ctx, h.pool, userID, func(tx pgx.Tx) error {
		var accessURL string
		var warningsRaw []byte
		qErr := tx.QueryRow(ctx, `
			SELECT COALESCE(encrypted_access_url, ''), last_synced_at, COALESCE(last_sync_warnings, '[]'::jsonb)
			FROM simplefin_connections
			WHERE workspace_id = $1
		`, prov.WorkspaceID).Scan(&accessURL, &sf.LastSyncedAt, &warningsRaw)
		if qErr != nil && !errors.Is(qErr, pgx.ErrNoRows) {
			return qErr
		}
		if qErr == nil && accessURL != "" {
			sf.Connected = true
		}
		if len(warningsRaw) > 0 {
			var parsed []string
			if jsonErr := json.Unmarshal(warningsRaw, &parsed); jsonErr == nil && parsed != nil {
				sf.LastSyncWarnings = parsed
			}
		}
		var hasDemoBridge bool
		if bErr := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM demo_simplefin_state WHERE workspace_id = $1)
		`, prov.WorkspaceID).Scan(&hasDemoBridge); bErr == nil {
			sf.IsDemoBridge = hasDemoBridge
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &SettingsResponse{
		WorkspaceID:         prov.WorkspaceID,
		WorkspaceName:       prov.WorkspaceName,
		IsDemo:              prov.IsDemo,
		DemoModeEnabled:     prov.DemoModeEnabled,
		InboundReceiptAlias: prov.InboundReceiptMail,
		SimpleFIN:           sf,
	}, nil
}

func (h *Handler) maybeTriggerSync(ctx context.Context, wsID uuid.UUID, syncNow bool) (string, string) {
	if !syncNow {
		return "", ""
	}
	tc, err := temporalinternal.NewClient(slog.Default())
	if err != nil {
		return "", "temporal_unavailable"
	}
	defer tc.Close()

	_ = schedules.EnsureWorkspaceSyncSchedule(ctx, tc, wsID)
	workflowID, err := schedules.TriggerSyncNow(ctx, tc, wsID)
	if err != nil {
		return "", "failed_to_trigger_sync"
	}
	return workflowID, ""
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	resp, err := h.loadSettingsResponse(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 255 {
		writeErr(w, http.StatusBadRequest, "workspace name must be 1 to 255 characters")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		wsID, err := db.ActiveWorkspaceID(r.Context(), tx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `UPDATE workspaces SET name = $2 WHERE id = $1`, wsID, name)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_workspace")
		return
	}

	resp, err := h.loadSettingsResponse(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ToggleDemo(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	// Ensure user has a provisioned real workspace before switching into or out of demo mode.
	if _, err := h.provisioner.EnsureWorkspaceProvisioned(r.Context(), h.pool, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_provision_workspace")
		return
	}

	tx, err := db.BeginWorkspaceTx(r.Context(), h.pool, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_begin_transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	// Record the preference before seeding. The workspace policies hide any workspace whose
	// is_demo does not match this user's demo_mode_enabled, so a demo workspace would be
	// invisible to its own seed inserts until the preference is switched on, which the
	// transaction commits or rolls back as one unit.
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO user_preferences (user_id, demo_mode_enabled, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET demo_mode_enabled = EXCLUDED.demo_mode_enabled,
		    updated_at = NOW()
	`, userID, body.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_demo_preference")
		return
	}

	if body.Enabled {
		var existingDemoID uuid.UUID
		err = tx.QueryRow(r.Context(), `
			SELECT w.id
			FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE wm.user_id = $1 AND w.is_demo = TRUE
			ORDER BY w.created_at ASC
			LIMIT 1
		`, userID).Scan(&existingDemoID)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := SeedDemoWorkspaceTx(r.Context(), tx, userID, h.mockBaseURL, h.vault); err != nil {
				writeErr(w, http.StatusInternalServerError, "failed_to_seed_demo_workspace")
				return
			}
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed_to_check_demo_workspace")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_commit_demo_toggle")
		return
	}

	resp, err := h.loadSettingsResponse(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ResetDemo(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if _, err := h.provisioner.EnsureWorkspaceProvisioned(r.Context(), h.pool, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_provision_workspace")
		return
	}

	tx, err := db.BeginWorkspaceTx(r.Context(), h.pool, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_begin_transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	// Switch the preference on before replacing the sandbox: the workspace policies hide a
	// demo workspace until demo_mode_enabled is true, and the delete below has to be able to
	// see the existing sandbox in order to remove it.
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO user_preferences (user_id, demo_mode_enabled, updated_at)
		VALUES ($1, TRUE, NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET demo_mode_enabled = TRUE,
		    updated_at = NOW()
	`, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_demo_preference")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		DELETE FROM workspaces
		WHERE id IN (
			SELECT w.id
			FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE wm.user_id = $1 AND w.is_demo = TRUE
		)
	`, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_demo_workspace")
		return
	}

	if _, err := SeedDemoWorkspaceTx(r.Context(), tx, userID, h.mockBaseURL, h.vault); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_seed_demo_workspace")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_commit_demo_reset")
		return
	}

	resp, err := h.loadSettingsResponse(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetSimulatorState(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	wsID, err := h.resolveActiveDemoWorkspace(r.Context(), userID)
	if err != nil {
		if errors.Is(err, errDemoModeNotActive) {
			writeErr(w, http.StatusBadRequest, "demo_mode_not_active")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_resolve_demo_workspace")
		return
	}

	state := SimulatorStateResponse{
		Accounts:           []SimAccount{},
		Transactions:       []SimTransaction{},
		ManualTransactions: []SimManualTransaction{},
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		acctRows, err := tx.Query(r.Context(), `
			SELECT id, external_account_id, name, currency, balance, account_type, card_mode
			FROM demo_simplefin_accounts
			WHERE workspace_id = $1
			ORDER BY created_at ASC
		`, wsID)
		if err != nil {
			return err
		}
		for acctRows.Next() {
			var a SimAccount
			var bal decimal.Decimal
			if err := acctRows.Scan(&a.ID, &a.ExternalAccountID, &a.Name, &a.Currency, &bal, &a.AccountType, &a.CardMode); err != nil {
				acctRows.Close()
				return err
			}
			a.Balance = bal.StringFixed(2)
			state.Accounts = append(state.Accounts, a)
		}
		acctRows.Close()
		if err := acctRows.Err(); err != nil {
			return err
		}

		txRows, err := tx.Query(r.Context(), `
			SELECT dst.id,
			       dst.external_account_id,
			       dst.external_transaction_id,
			       dst.posted_at,
			       dst.amount,
			       dst.description,
			       dst.pending,
			       EXISTS (
			           SELECT 1 FROM transactions t
			           WHERE t.workspace_id = $1
			             AND t.external_transaction_id = dst.external_transaction_id
			       ) AS is_synced
			FROM demo_simplefin_transactions dst
			WHERE dst.workspace_id = $1
			ORDER BY dst.posted_at DESC, dst.created_at DESC
			LIMIT 100
		`, wsID)
		if err != nil {
			return err
		}
		for txRows.Next() {
			var st SimTransaction
			var amt decimal.Decimal
			if err := txRows.Scan(&st.ID, &st.ExternalAccountID, &st.ExternalTransactionID, &st.PostedAt, &amt, &st.Description, &st.Pending, &st.IsSynced); err != nil {
				txRows.Close()
				return err
			}
			st.Amount = amt.StringFixed(2)
			state.Transactions = append(state.Transactions, st)
		}
		txRows.Close()
		if err := txRows.Err(); err != nil {
			return err
		}

		if err := tx.QueryRow(r.Context(), `
			SELECT COUNT(*)
			FROM demo_simplefin_transactions dst
			WHERE dst.workspace_id = $1
			  AND NOT EXISTS (
			      SELECT 1 FROM transactions t
			      WHERE t.workspace_id = $1
			        AND t.external_transaction_id = dst.external_transaction_id
			  )
		`, wsID).Scan(&state.UnsyncedCount); err != nil {
			return err
		}

		manRows, err := tx.Query(r.Context(), `
			SELECT t.id, t.account_id, a.name, a.external_account_id, t.amount, t.merchant_raw, t.transaction_date, t.status
			FROM transactions t
			JOIN accounts a ON a.id = t.account_id
			WHERE t.workspace_id = $1
			  AND a.workspace_id = $1
			  AND t.parent_transaction_id IS NULL
			  AND t.origin = 'MANUAL_OFFLINE'
			ORDER BY t.transaction_date DESC, t.created_at DESC
			LIMIT 50
		`, wsID)
		if err != nil {
			return err
		}
		for manRows.Next() {
			var m SimManualTransaction
			var amt decimal.Decimal
			var txDate time.Time
			if err := manRows.Scan(&m.ID, &m.AccountID, &m.AccountName, &m.ExternalAccountID, &amt, &m.MerchantRaw, &txDate, &m.Status); err != nil {
				manRows.Close()
				return err
			}
			m.Amount = amt.StringFixed(2)
			m.TransactionDate = txDate.UTC().Format("2006-01-02")
			state.ManualTransactions = append(state.ManualTransactions, m)
		}
		manRows.Close()
		if err := manRows.Err(); err != nil {
			return err
		}

		err = tx.QueryRow(r.Context(), `
			SELECT fault_mode, fault_persistent, fault_retry_after_seconds, fault_message
			FROM demo_simplefin_state
			WHERE workspace_id = $1
		`, wsID).Scan(&state.Fault.Mode, &state.Fault.Persistent, &state.Fault.RetryAfterSeconds, &state.Fault.Message)
		if errors.Is(err, pgx.ErrNoRows) {
			state.Fault = SimFaultState{Mode: FaultNone, RetryAfterSeconds: 3}
			return nil
		}
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_simulator")
		return
	}

	writeJSON(w, http.StatusOK, state)
}

type stageTxPreset struct {
	extAcctID   string
	amount      decimal.Decimal
	description string
	pending     bool
}

var txPresets = map[string]stageTxPreset{
	"coffee": {
		extAcctID:   "demo-cc-rewards-001",
		amount:      decimal.RequireFromString("-6.50"),
		description: "SQ *ARTISAN COFFEE ROASTERS",
		pending:     false,
	},
	"groceries": {
		extAcctID:   "demo-cc-rewards-001",
		amount:      decimal.RequireFromString("-84.30"),
		description: "POS DEBIT WHOLE FOODS MKT #102",
		pending:     false,
	},
	"dining_pending": {
		extAcctID:   "demo-cc-rewards-001",
		amount:      decimal.RequireFromString("-42.00"),
		description: "BISTRO MODERNE",
		pending:     true,
	},
	"paycheck": {
		extAcctID:   "demo-checking-001",
		amount:      decimal.RequireFromString("2600.00"),
		description: "DIRECT DEP ACME CORP PAYROLL",
		pending:     false,
	},
	"utility": {
		extAcctID:   "demo-checking-001",
		amount:      decimal.RequireFromString("-115.00"),
		description: "METRO FIBER INTERNET AUTOPAY",
		pending:     false,
	},
}

func parsePostedAtInput(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Now().UTC(), nil
	}
	if len(trimmed) == 10 {
		t, err := time.Parse("2006-01-02", trimmed)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC), nil
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func (h *Handler) StageTransaction(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wsID, err := h.resolveActiveDemoWorkspace(r.Context(), userID)
	if err != nil {
		if errors.Is(err, errDemoModeNotActive) {
			writeErr(w, http.StatusBadRequest, "demo_mode_not_active")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_resolve_demo_workspace")
		return
	}

	var req struct {
		Preset            string          `json:"preset"`
		ExternalAccountID string          `json:"external_account_id"`
		Description       string          `json:"description"`
		Amount            decimal.Decimal `json:"amount"`
		PostedAt          string          `json:"posted_at"`
		Pending           bool            `json:"pending"`
		SyncNow           bool            `json:"sync_now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	extAcctID := strings.TrimSpace(req.ExternalAccountID)
	desc := strings.TrimSpace(req.Description)
	amt := req.Amount.Round(2)
	pending := req.Pending
	postedAt, err := parsePostedAtInput(req.PostedAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid posted_at format")
		return
	}

	if req.Preset != "" {
		p, ok := txPresets[req.Preset]
		if !ok {
			writeErr(w, http.StatusBadRequest, "unknown preset")
			return
		}
		if extAcctID == "" {
			extAcctID = p.extAcctID
		}
		if desc == "" {
			desc = p.description
		}
		if amt.IsZero() {
			amt = p.amount
		}
		if !req.Pending {
			pending = p.pending
		}
		if req.PostedAt == "" {
			postedAt = time.Now().UTC()
		}
	}

	if extAcctID == "" || desc == "" || amt.IsZero() {
		writeErr(w, http.StatusBadRequest, "external_account_id, description, and non-zero amount are required")
		return
	}

	extTxID := "demo-tx-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	txID := uuid.New()

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `
			UPDATE demo_simplefin_accounts
			SET balance = balance + $3
			WHERE workspace_id = $1 AND external_account_id = $2
		`, wsID, extAcctID, amt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}

		_, err = tx.Exec(r.Context(), `
			INSERT INTO demo_simplefin_transactions (
				id, workspace_id, external_account_id, external_transaction_id,
				posted_at, amount, description, pending
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, txID, wsID, extAcctID, extTxID, postedAt, amt, desc, pending)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "demo_account_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_stage_transaction")
		return
	}

	workflowID, syncErr := h.maybeTriggerSync(r.Context(), wsID, req.SyncNow)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                      txID,
		"external_transaction_id": extTxID,
		"workflow_id":             workflowID,
		"sync_error":              syncErr,
	})
}

func (h *Handler) SettlePending(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wsID, err := h.resolveActiveDemoWorkspace(r.Context(), userID)
	if err != nil {
		if errors.Is(err, errDemoModeNotActive) {
			writeErr(w, http.StatusBadRequest, "demo_mode_not_active")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_resolve_demo_workspace")
		return
	}

	var req struct {
		DemoTransactionID uuid.UUID        `json:"demo_transaction_id"`
		SettledAmount     *decimal.Decimal `json:"settled_amount"`
		TipPercent        *decimal.Decimal `json:"tip_percent"`
		NewDescription    string           `json:"new_description"`
		AppendSuffix      string           `json:"append_suffix"`
		SyncNow           bool             `json:"sync_now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DemoTransactionID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "demo_transaction_id is required")
		return
	}

	newExtTxID := "demo-tx-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var extAcctID, oldDesc string
		var oldAmt decimal.Decimal
		var pending bool
		err := tx.QueryRow(r.Context(), `
			SELECT external_account_id, amount, description, pending
			FROM demo_simplefin_transactions
			WHERE id = $1 AND workspace_id = $2
			FOR UPDATE
		`, req.DemoTransactionID, wsID).Scan(&extAcctID, &oldAmt, &oldDesc, &pending)
		if err != nil {
			return err
		}
		if !pending {
			return fmt.Errorf("transaction_already_posted")
		}

		newAmt := oldAmt
		if req.SettledAmount != nil {
			newAmt = req.SettledAmount.Round(2)
		} else if req.TipPercent != nil {
			factor := decimal.NewFromInt(1).Add(req.TipPercent.Div(decimal.NewFromInt(100)))
			newAmt = oldAmt.Mul(factor).Round(2)
		}

		newDesc := oldDesc
		if trimmed := strings.TrimSpace(req.NewDescription); trimmed != "" {
			newDesc = trimmed
		} else if suffix := strings.TrimSpace(req.AppendSuffix); suffix != "" {
			newDesc = strings.TrimSpace(oldDesc) + " " + suffix
		}

		delta := newAmt.Sub(oldAmt).Round(2)
		if !delta.IsZero() {
			if _, err := tx.Exec(r.Context(), `
				UPDATE demo_simplefin_accounts
				SET balance = balance + $3
				WHERE workspace_id = $1 AND external_account_id = $2
			`, wsID, extAcctID, delta); err != nil {
				return err
			}
		}

		_, err = tx.Exec(r.Context(), `
			UPDATE demo_simplefin_transactions
			SET external_transaction_id = $3,
			    amount = $4,
			    description = $5,
			    pending = FALSE,
			    posted_at = NOW()
			WHERE id = $1 AND workspace_id = $2
		`, req.DemoTransactionID, wsID, newExtTxID, newAmt, newDesc)
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "pending_transaction_not_found")
			return
		}
		if err.Error() == "transaction_already_posted" {
			writeErr(w, http.StatusBadRequest, "transaction is already posted")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_settle_pending_transaction")
		return
	}

	workflowID, syncErr := h.maybeTriggerSync(r.Context(), wsID, req.SyncNow)
	writeJSON(w, http.StatusOK, map[string]any{
		"external_transaction_id": newExtTxID,
		"workflow_id":             workflowID,
		"sync_error":              syncErr,
	})
}

func (h *Handler) StageTransfer(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wsID, err := h.resolveActiveDemoWorkspace(r.Context(), userID)
	if err != nil {
		if errors.Is(err, errDemoModeNotActive) {
			writeErr(w, http.StatusBadRequest, "demo_mode_not_active")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_resolve_demo_workspace")
		return
	}

	var req struct {
		FromExternalAccountID string          `json:"from_external_account_id"`
		ToExternalAccountID   string          `json:"to_external_account_id"`
		Amount                decimal.Decimal `json:"amount"`
		DayOffset             int             `json:"day_offset"`
		Description           string          `json:"description"`
		SyncNow               bool            `json:"sync_now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	fromID := strings.TrimSpace(req.FromExternalAccountID)
	toID := strings.TrimSpace(req.ToExternalAccountID)
	amt := req.Amount.Round(2)
	if fromID == "" || toID == "" || fromID == toID || !amt.IsPositive() {
		writeErr(w, http.StatusBadRequest, "distinct from/to accounts and positive amount are required")
		return
	}
	if req.DayOffset < 0 || req.DayOffset > 3 {
		writeErr(w, http.StatusBadRequest, "day_offset must be between 0 and 3")
		return
	}

	baseTime := time.Now().UTC()
	outTime := baseTime.AddDate(0, 0, -req.DayOffset)
	inTime := baseTime
	outExtID := "demo-tx-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	inExtID := "demo-tx-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var fromName, toName string
		if err := tx.QueryRow(r.Context(), `
			SELECT name FROM demo_simplefin_accounts WHERE workspace_id = $1 AND external_account_id = $2
		`, wsID, fromID).Scan(&fromName); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `
			SELECT name FROM demo_simplefin_accounts WHERE workspace_id = $1 AND external_account_id = $2
		`, wsID, toID).Scan(&toName); err != nil {
			return err
		}

		outDesc := strings.TrimSpace(req.Description)
		inDesc := outDesc
		if outDesc == "" {
			outDesc = "ACH TRANSFER TO " + strings.ToUpper(toName)
			inDesc = "ACH TRANSFER FROM " + strings.ToUpper(fromName)
		}

		if _, err := tx.Exec(r.Context(), `
			UPDATE demo_simplefin_accounts SET balance = balance - $3
			WHERE workspace_id = $1 AND external_account_id = $2
		`, wsID, fromID, amt); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE demo_simplefin_accounts SET balance = balance + $3
			WHERE workspace_id = $1 AND external_account_id = $2
		`, wsID, toID, amt); err != nil {
			return err
		}

		if _, err := tx.Exec(r.Context(), `
			INSERT INTO demo_simplefin_transactions (
				workspace_id, external_account_id, external_transaction_id, posted_at, amount, description, pending
			) VALUES ($1, $2, $3, $4, $5, $6, FALSE),
			         ($1, $7, $8, $9, $10, $11, FALSE)
		`, wsID, fromID, outExtID, outTime, amt.Neg(), outDesc,
			toID, inExtID, inTime, amt, inDesc); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "demo_account_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_stage_transfer")
		return
	}

	workflowID, syncErr := h.maybeTriggerSync(r.Context(), wsID, req.SyncNow)
	writeJSON(w, http.StatusCreated, map[string]any{
		"outflow_external_id": outExtID,
		"inflow_external_id":  inExtID,
		"workflow_id":         workflowID,
		"sync_error":          syncErr,
	})
}

func (h *Handler) StageManualMatch(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wsID, err := h.resolveActiveDemoWorkspace(r.Context(), userID)
	if err != nil {
		if errors.Is(err, errDemoModeNotActive) {
			writeErr(w, http.StatusBadRequest, "demo_mode_not_active")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_resolve_demo_workspace")
		return
	}

	var req struct {
		ManualTransactionID uuid.UUID       `json:"manual_transaction_id"`
		TipPercent          decimal.Decimal `json:"tip_percent"`
		DayOffset           int             `json:"day_offset"`
		SyncNow             bool            `json:"sync_now"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ManualTransactionID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, "manual_transaction_id is required")
		return
	}
	if req.DayOffset < -2 || req.DayOffset > 2 {
		writeErr(w, http.StatusBadRequest, "day_offset must be between -2 and 2")
		return
	}
	if req.TipPercent.LessThan(decimal.Zero) || req.TipPercent.GreaterThan(decimal.NewFromInt(25)) {
		writeErr(w, http.StatusBadRequest, "tip_percent must be between 0 and 25")
		return
	}

	extTxID := "demo-tx-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var extAcctID *string
		var origAmt decimal.Decimal
		var merchantRaw string
		var txDate time.Time
		err := tx.QueryRow(r.Context(), `
			SELECT a.external_account_id, t.amount, t.merchant_raw, t.transaction_date
			FROM transactions t
			JOIN accounts a ON a.id = t.account_id
			WHERE t.id = $1
			  AND t.workspace_id = $2
			  AND a.workspace_id = $2
			  AND t.origin = 'MANUAL_OFFLINE'
		`, req.ManualTransactionID, wsID).Scan(&extAcctID, &origAmt, &merchantRaw, &txDate)
		if err != nil {
			return err
		}
		if extAcctID == nil || *extAcctID == "" {
			return fmt.Errorf("account_not_linked")
		}

		factor := decimal.NewFromInt(1).Add(req.TipPercent.Div(decimal.NewFromInt(100)))
		bankAmt := origAmt.Mul(factor).Round(2)
		postedAt := time.Date(txDate.Year(), txDate.Month(), txDate.Day(), 12, 0, 0, 0, time.UTC).AddDate(0, 0, req.DayOffset)

		tag, err := tx.Exec(r.Context(), `
			UPDATE demo_simplefin_accounts SET balance = balance + $3
			WHERE workspace_id = $1 AND external_account_id = $2
		`, wsID, *extAcctID, bankAmt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("demo_account_missing")
		}

		_, err = tx.Exec(r.Context(), `
			INSERT INTO demo_simplefin_transactions (
				workspace_id, external_account_id, external_transaction_id,
				posted_at, amount, description, pending
			) VALUES ($1, $2, $3, $4, $5, $6, FALSE)
		`, wsID, *extAcctID, extTxID, postedAt, bankAmt, strings.ToUpper(merchantRaw))
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "manual_transaction_not_found")
			return
		}
		if err.Error() == "account_not_linked" || err.Error() == "demo_account_missing" {
			writeErr(w, http.StatusUnprocessableEntity, "manual transaction account is not linked to demo SimpleFIN bridge")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_stage_manual_match")
		return
	}

	workflowID, syncErr := h.maybeTriggerSync(r.Context(), wsID, req.SyncNow)
	writeJSON(w, http.StatusCreated, map[string]any{
		"external_transaction_id": extTxID,
		"workflow_id":             workflowID,
		"sync_error":              syncErr,
	})
}

func (h *Handler) UpdateFault(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	wsID, err := h.resolveActiveDemoWorkspace(r.Context(), userID)
	if err != nil {
		if errors.Is(err, errDemoModeNotActive) {
			writeErr(w, http.StatusBadRequest, "demo_mode_not_active")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_resolve_demo_workspace")
		return
	}

	var req SimFaultState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	switch req.Mode {
	case FaultNone, FaultRateLimit429, FaultPartialWarning, FaultAuthError403:
	default:
		writeErr(w, http.StatusBadRequest, "invalid fault mode")
		return
	}
	retry := req.RetryAfterSeconds
	if retry < 1 {
		retry = 3
	} else if retry > 60 {
		retry = 60
	}
	msg := strings.TrimSpace(req.Message)

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `
			UPDATE demo_simplefin_state
			SET fault_mode = $2,
			    fault_persistent = $3,
			    fault_retry_after_seconds = $4,
			    fault_message = $5,
			    updated_at = NOW()
			WHERE workspace_id = $1
		`, wsID, req.Mode, req.Persistent, retry, msg)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_fault_state")
		return
	}

	req.RetryAfterSeconds = retry
	req.Message = msg
	writeJSON(w, http.StatusOK, req)
}
