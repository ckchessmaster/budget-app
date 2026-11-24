package demo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type stubBridgeRecord struct {
	username        string
	password        string
	faultMode       string
	faultPersistent bool
	retryAfter      int
	faultMessage    string
	accounts        []simplefin.AccountDTO
	transactions    []stubBridgeTx
}

type stubBridgeTx struct {
	dto      simplefin.TransactionDTO
	postedAt time.Time
}

type memoryMockBridgeStore struct {
	bridges map[uuid.UUID]*stubBridgeRecord
}

func (m *memoryMockBridgeStore) LoadBridgeData(
	_ context.Context,
	workspaceID uuid.UUID,
	username, password string,
	startDate *time.Time,
) (*MockBridgeLoadResult, error) {
	rec, ok := m.bridges[workspaceID]
	if !ok {
		return nil, ErrBridgeNotFound
	}
	if username != rec.username || password != rec.password {
		return nil, ErrBridgeUnauthorized
	}

	fault := rec.faultMode
	if fault != FaultNone && !rec.faultPersistent {
		rec.faultMode = FaultNone
	}

	if fault == FaultRateLimit429 || fault == FaultAuthError403 {
		return &MockBridgeLoadResult{
			FaultMode:       fault,
			FaultRetryAfter: rec.retryAfter,
			FaultMessage:    rec.faultMessage,
		}, nil
	}

	outAccts := make([]simplefin.AccountDTO, len(rec.accounts))
	idxMap := make(map[string]int, len(rec.accounts))
	for i, a := range rec.accounts {
		outAccts[i] = a
		outAccts[i].Transactions = nil
		idxMap[a.ID] = i
	}

	for _, t := range rec.transactions {
		if startDate != nil && !t.dto.Pending && t.postedAt.Before(*startDate) {
			continue
		}
		if idx, exists := idxMap[t.dto.AccountID]; exists {
			outAccts[idx].Transactions = append(outAccts[idx].Transactions, t.dto)
		}
	}

	var warnings []string
	if fault == FaultPartialWarning {
		msg := rec.faultMessage
		if msg == "" {
			msg = "Demo Bank warning: partial outage"
		}
		warnings = append(warnings, msg)
	}

	return &MockBridgeLoadResult{
		FaultMode: fault,
		Data: &simplefin.DataResponse{
			Errors:   warnings,
			Accounts: outAccts,
		},
	}, nil
}

func TestMockBridgeHandlerAndSimpleFINClientRoundTrip(t *testing.T) {
	wsID := uuid.New()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	store := &memoryMockBridgeStore{
		bridges: map[uuid.UUID]*stubBridgeRecord{
			wsID: {
				username:   "demouser",
				password:   "demopass",
				faultMode:  FaultNone,
				retryAfter: 5,
				accounts: []simplefin.AccountDTO{
					{ID: "demo-checking-001", Name: "Demo Checking", Currency: "USD", Balance: "4250.00"},
				},
				transactions: []stubBridgeTx{
					{
						dto: simplefin.TransactionDTO{
							ID:          "tx-old",
							AccountID:   "demo-checking-001",
							Posted:      now.AddDate(0, 0, -20).Unix(),
							Amount:      "-15.00",
							Description: "OLD COFFEE",
							Pending:     false,
						},
						postedAt: now.AddDate(0, 0, -20),
					},
					{
						dto: simplefin.TransactionDTO{
							ID:          "tx-pending-old",
							AccountID:   "demo-checking-001",
							Posted:      now.AddDate(0, 0, -15).Unix(),
							Amount:      "-40.00",
							Description: "BURGER BAR",
							Pending:     true,
						},
						postedAt: now.AddDate(0, 0, -15),
					},
					{
						dto: simplefin.TransactionDTO{
							ID:          "tx-recent",
							AccountID:   "demo-checking-001",
							Posted:      now.AddDate(0, 0, -1).Unix(),
							Amount:      "-84.30",
							Description: "WHOLE FOODS",
							Pending:     false,
						},
						postedAt: now.AddDate(0, 0, -1),
					},
				},
			},
		},
	}

	r := chi.NewRouter()
	h := NewMockBridgeHandler(store)
	r.Get("/mock-simplefin/{workspace_id}/accounts", h.GetAccounts)

	srv := httptest.NewServer(r)
	defer srv.Close()

	client := simplefin.NewClient(srv.Client())
	ctx := context.Background()

	t.Run("rejects bad basic auth with AuthError", func(t *testing.T) {
		badURL := "http://wrong:creds@" + srv.Listener.Addr().String() + "/mock-simplefin/" + wsID.String()
		_, err := client.FetchData(ctx, badURL, time.Time{})
		var authErr *simplefin.AuthError
		if err == nil || !errorAs(err, &authErr) || authErr.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 *simplefin.AuthError, got %v", err)
		}
	})

	t.Run("filters by start-date while keeping pending transactions", func(t *testing.T) {
		validURL := "http://demouser:demopass@" + srv.Listener.Addr().String() + "/mock-simplefin/" + wsID.String()
		data, err := client.FetchData(ctx, validURL, now.AddDate(0, 0, -7))
		if err != nil {
			t.Fatalf("FetchData failed: %v", err)
		}
		if len(data.Accounts) != 1 || data.Accounts[0].Name != "Demo Checking" {
			t.Fatalf("unexpected accounts: %+v", data.Accounts)
		}
		// Expect tx-pending-old (pending=true) + tx-recent (posted >= now-7d), excluding tx-old
		if len(data.Transactions) != 2 {
			t.Fatalf("expected 2 transactions, got %d: %+v", len(data.Transactions), data.Transactions)
		}
	})

	t.Run("one-shot 429 rate limit resets after first sync", func(t *testing.T) {
		rec := store.bridges[wsID]
		rec.faultMode = FaultRateLimit429
		rec.faultPersistent = false
		rec.retryAfter = 7

		validURL := "http://demouser:demopass@" + srv.Listener.Addr().String() + "/mock-simplefin/" + wsID.String()
		_, err := client.FetchData(ctx, validURL, time.Time{})
		var rlErr *simplefin.RateLimitError
		if err == nil || !errorAs(err, &rlErr) || rlErr.RetryAfter != 7*time.Second {
			t.Fatalf("expected 7s RateLimitError, got %v", err)
		}

		// Second call should succeed cleanly
		data, err := client.FetchData(ctx, validURL, time.Time{})
		if err != nil || len(data.Transactions) != 3 {
			t.Fatalf("expected recovery on second call, got err=%v txs=%d", err, len(data.Transactions))
		}
	})

	t.Run("persistent 403 fault stays active across requests", func(t *testing.T) {
		rec := store.bridges[wsID]
		rec.faultMode = FaultAuthError403
		rec.faultPersistent = true

		validURL := "http://demouser:demopass@" + srv.Listener.Addr().String() + "/mock-simplefin/" + wsID.String()
		for i := 0; i < 2; i++ {
			_, err := client.FetchData(ctx, validURL, time.Time{})
			var authErr *simplefin.AuthError
			if err == nil || !errorAs(err, &authErr) || authErr.StatusCode != http.StatusForbidden {
				t.Fatalf("call %d: expected 403 *simplefin.AuthError, got %v", i+1, err)
			}
		}
		rec.faultMode = FaultNone
		rec.faultPersistent = false
	})

	t.Run("partial warning appends to DataResponse.Errors", func(t *testing.T) {
		rec := store.bridges[wsID]
		rec.faultMode = FaultPartialWarning
		rec.faultMessage = "Institution maintenance warning"

		validURL := "http://demouser:demopass@" + srv.Listener.Addr().String() + "/mock-simplefin/" + wsID.String()
		data, err := client.FetchData(ctx, validURL, time.Time{})
		if err != nil {
			t.Fatalf("FetchData failed: %v", err)
		}
		if len(data.Errors) != 1 || data.Errors[0] != "Institution maintenance warning" {
			t.Fatalf("unexpected warnings: %+v", data.Errors)
		}
	})
}

func errorAs[T error](err error, target *T) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(T); ok {
		*target = e
		return true
	}
	return false
}
