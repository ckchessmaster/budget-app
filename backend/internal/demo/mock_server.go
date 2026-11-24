package demo

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const (
	FaultNone           = "NONE"
	FaultRateLimit429   = "RATE_LIMIT_429"
	FaultPartialWarning = "PARTIAL_WARNING"
	FaultAuthError403   = "AUTH_ERROR_403"
)

var (
	ErrBridgeNotFound     = errors.New("demo simplefin bridge not found")
	ErrBridgeUnauthorized = errors.New("invalid simplefin bridge credentials")
)

type MockBridgeLoadResult struct {
	FaultMode       string
	FaultRetryAfter int
	FaultMessage    string
	Data            *simplefin.DataResponse
}

type MockBridgeStore interface {
	LoadBridgeData(ctx context.Context, workspaceID uuid.UUID, username, password string, startDate *time.Time) (*MockBridgeLoadResult, error)
}

type pgMockBridgeStore struct {
	pool *pgxpool.Pool
}

func NewMockBridgeStore(pool *pgxpool.Pool) MockBridgeStore {
	return &pgMockBridgeStore{pool: pool}
}

func (s *pgMockBridgeStore) LoadBridgeData(
	ctx context.Context,
	workspaceID uuid.UUID,
	username, password string,
	startDate *time.Time,
) (*MockBridgeLoadResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The demo_simplefin_* policies isolate rows by workspace visibility, which needs
	// app.current_user_id, and this caller has none: the bridge authenticates with the
	// credentials SimpleFIN carries in the access URL, not with an OIDC subject. These
	// transaction-local settings are the credential arm those policies match against.
	// is_local is what makes this safe on a pooled connection -- the values disappear at
	// commit or rollback instead of leaking into whoever checks the connection out next.
	if _, err := tx.Exec(ctx, `
		SELECT set_config('app.demo_bridge_workspace_id', $1, true),
		       set_config('app.demo_bridge_username', $2, true),
		       set_config('app.demo_bridge_password', $3, true)
	`, workspaceID.String(), username, password); err != nil {
		return nil, err
	}

	var expectedUser, expectedPass, faultMode, faultMessage string
	var faultPersistent bool
	var retryAfter int
	err = tx.QueryRow(ctx, `
		SELECT auth_username, auth_password, fault_mode, fault_persistent,
		       fault_retry_after_seconds, fault_message
		FROM demo_simplefin_state
		WHERE workspace_id = $1
		FOR UPDATE
	`, workspaceID).Scan(&expectedUser, &expectedPass, &faultMode, &faultPersistent, &retryAfter, &faultMessage)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBridgeNotFound
		}
		return nil, err
	}

	// Reaching this point already means the credential arm matched, since the row is
	// invisible otherwise. The comparison stays as a second layer so the store cannot
	// serve data if a policy is ever widened.
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(expectedUser)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(expectedPass)) == 1
	if !userOK || !passOK {
		return nil, ErrBridgeUnauthorized
	}

	if faultMode != FaultNone && !faultPersistent {
		if _, err := tx.Exec(ctx, `
			UPDATE demo_simplefin_state
			SET fault_mode = 'NONE', updated_at = NOW()
			WHERE workspace_id = $1
		`, workspaceID); err != nil {
			return nil, err
		}
	}

	if faultMode == FaultRateLimit429 || faultMode == FaultAuthError403 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &MockBridgeLoadResult{
			FaultMode:       faultMode,
			FaultRetryAfter: retryAfter,
			FaultMessage:    faultMessage,
		}, nil
	}

	acctRows, err := tx.Query(ctx, `
		SELECT external_account_id, name, currency, balance
		FROM demo_simplefin_accounts
		WHERE workspace_id = $1
		ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}

	var accounts []simplefin.AccountDTO
	acctIndex := make(map[string]int)
	for acctRows.Next() {
		var a simplefin.AccountDTO
		var bal decimal.Decimal
		if err := acctRows.Scan(&a.ID, &a.Name, &a.Currency, &bal); err != nil {
			acctRows.Close()
			return nil, err
		}
		a.Balance = bal.StringFixed(2)
		acctIndex[a.ID] = len(accounts)
		accounts = append(accounts, a)
	}
	acctRows.Close()
	if err := acctRows.Err(); err != nil {
		return nil, err
	}

	var txRows pgx.Rows
	if startDate != nil {
		txRows, err = tx.Query(ctx, `
			SELECT external_account_id, external_transaction_id, posted_at, amount, description, pending
			FROM demo_simplefin_transactions
			WHERE workspace_id = $1
			  AND (pending = TRUE OR posted_at >= $2)
			ORDER BY posted_at DESC, created_at DESC
		`, workspaceID, startDate.UTC())
	} else {
		txRows, err = tx.Query(ctx, `
			SELECT external_account_id, external_transaction_id, posted_at, amount, description, pending
			FROM demo_simplefin_transactions
			WHERE workspace_id = $1
			ORDER BY posted_at DESC, created_at DESC
		`, workspaceID)
	}
	if err != nil {
		return nil, err
	}

	for txRows.Next() {
		var extAcctID, extTxID, desc string
		var postedAt time.Time
		var amt decimal.Decimal
		var pending bool
		if err := txRows.Scan(&extAcctID, &extTxID, &postedAt, &amt, &desc, &pending); err != nil {
			txRows.Close()
			return nil, err
		}
		idx, ok := acctIndex[extAcctID]
		if !ok {
			continue
		}
		accounts[idx].Transactions = append(accounts[idx].Transactions, simplefin.TransactionDTO{
			ID:          extTxID,
			AccountID:   extAcctID,
			Posted:      postedAt.UTC().Unix(),
			Amount:      amt.StringFixed(2),
			Description: desc,
			Pending:     pending,
		})
	}
	txRows.Close()
	if err := txRows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	var warnings []string
	if faultMode == FaultPartialWarning {
		msg := strings.TrimSpace(faultMessage)
		if msg == "" {
			msg = "Demo Bank warning: Legacy Card institution maintenance; balances may be delayed."
		}
		warnings = append(warnings, msg)
	}

	return &MockBridgeLoadResult{
		FaultMode: faultMode,
		Data: &simplefin.DataResponse{
			Errors:   warnings,
			Accounts: accounts,
		},
	}, nil
}

type MockBridgeHandler struct {
	store MockBridgeStore
}

func NewMockBridgeHandler(store MockBridgeStore) *MockBridgeHandler {
	return &MockBridgeHandler{store: store}
}

func (h *MockBridgeHandler) GetAccounts(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "workspace_id"))
	if err != nil {
		http.Error(w, "invalid workspace id", http.StatusBadRequest)
		return
	}

	user, pass, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Demo SimpleFIN"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var startDate *time.Time
	if rawStart := strings.TrimSpace(r.URL.Query().Get("start-date")); rawStart != "" {
		if unixSecs, err := strconv.ParseInt(rawStart, 10, 64); err == nil && unixSecs > 0 {
			t := time.Unix(unixSecs, 0).UTC()
			startDate = &t
		}
	}

	res, err := h.store.LoadBridgeData(r.Context(), wsID, user, pass, startDate)
	if err != nil {
		if errors.Is(err, ErrBridgeNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrBridgeUnauthorized) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Demo SimpleFIN"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	switch res.FaultMode {
	case FaultRateLimit429:
		retry := res.FaultRetryAfter
		if retry < 1 {
			retry = 3
		}
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		msg := strings.TrimSpace(res.FaultMessage)
		if msg == "" {
			msg = "Demo SimpleFIN rate limit exceeded"
		}
		http.Error(w, msg, http.StatusTooManyRequests)
		return
	case FaultAuthError403:
		msg := strings.TrimSpace(res.FaultMessage)
		if msg == "" {
			msg = "Demo SimpleFIN access revoked or expired"
		}
		http.Error(w, msg, http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res.Data)
}
