package transaction

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// recordingReplay stands in for the Temporal starter so the endpoint can be tested without
// a server. What it recorded is the contract: a scope whose workspace id is real, and items
// that each carry the key the client queued them under.
type recordingReplay struct {
	mu      sync.Mutex
	scopes  []temporalinternal.SyncScope
	batches [][]temporalinternal.ReplayItem
	id      string
	err     error
}

func (r *recordingReplay) Start(_ context.Context, scope temporalinternal.SyncScope, items []temporalinternal.ReplayItem) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopes = append(r.scopes, scope)
	r.batches = append(r.batches, items)
	if r.err != nil {
		return "", r.err
	}
	return r.id, nil
}

func (r *recordingReplay) itemCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, batch := range r.batches {
		total += len(batch)
	}
	return total
}

func (r *recordingReplay) lastBatch() []temporalinternal.ReplayItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.batches) == 0 {
		return nil
	}
	return r.batches[len(r.batches)-1]
}

// authedRouter mounts the transaction routes with a caller already on the request, so these
// tests exercise the handler rather than the auth middleware. The edge cases pass a nil
// pool on purpose: every one of them must reject before anything touches the database.
func authedRouter(pool *pgxpool.Pool, replay ReplayWorkflow, userID uuid.UUID) chi.Router {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			reqCtx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
			next.ServeHTTP(w, req.WithContext(reqCtx))
		})
	})
	RegisterRoutes(r, pool, replay)
	return r
}

func syncRequest(t *testing.T, body string, replay ReplayWorkflow) *httptest.ResponseRecorder {
	t.Helper()

	r := authedRouter(nil, replay, uuid.New())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/sync", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func queuedItem(clientID, localID string) OfflineSyncItem {
	return OfflineSyncItem{
		ClientID:      clientID,
		LegacyLocalID: localID,
		AccountID:     uuid.New(),
		Amount:        decimal.NewFromFloat(-12.50),
		MerchantRaw:   "Corner Store",
	}
}

func TestSyncOfflineTransactions_RejectsBatchesAtTheEdge(t *testing.T) {
	t.Run("empty batch", func(t *testing.T) {
		rec := syncRequest(t, `{"items":[]}`, &recordingReplay{id: "wf-1"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "empty_batch") {
			t.Fatalf("body = %s, want empty_batch", rec.Body.String())
		}
	})

	t.Run("oversized batch", func(t *testing.T) {
		items := make([]OfflineSyncItem, temporalinternal.MaxReplayBatchItems+1)
		for i := range items {
			items[i] = queuedItem(fmt.Sprintf("key-%d", i), "")
		}
		body, err := json.Marshal(SyncTransactionsRequest{Items: items})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		replay := &recordingReplay{id: "wf-1"}
		rec := syncRequest(t, string(body), replay)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d body=%s, want 413", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "batch_too_large") {
			t.Fatalf("body = %s, want batch_too_large", rec.Body.String())
		}
		if replay.itemCount() != 0 {
			t.Fatalf("an oversized batch must never reach the workflow")
		}
	})

	t.Run("item without an idempotency key", func(t *testing.T) {
		body, err := json.Marshal(SyncTransactionsRequest{Items: []OfflineSyncItem{
			queuedItem("key-1", ""),
			queuedItem("", ""),
		}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		replay := &recordingReplay{id: "wf-1"}
		rec := syncRequest(t, string(body), replay)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d body=%s, want 422", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), ErrClientKeyRequired.Error()) {
			t.Fatalf("body = %s, want %s", rec.Body.String(), ErrClientKeyRequired.Error())
		}
		if !strings.Contains(rec.Body.String(), `"index":1`) {
			t.Fatalf("body = %s, want the offending item index reported", rec.Body.String())
		}
		if replay.itemCount() != 0 {
			t.Fatalf("a batch with an unkeyable item must be rejected whole")
		}
	})

	t.Run("no temporal client configured", func(t *testing.T) {
		body, err := json.Marshal(SyncTransactionsRequest{Items: []OfflineSyncItem{queuedItem("key-1", "")}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rec := syncRequest(t, string(body), nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "temporal_unavailable") {
			t.Fatalf("body = %s, want temporal_unavailable", rec.Body.String())
		}
	})
}

func TestSyncOfflineTransactions_QueuesBatchAndReturnsWorkflowID(t *testing.T) {
	_, appPool, wsID, userID := openTestDB(t)

	replay := &recordingReplay{id: "offline-replay-abc"}
	r := authedRouter(appPool, replay, userID)

	// The second entry uses the pre-TASK-5.2.4 spelling. It must still arrive with a key:
	// an old bundle that loses its key on every retry is the duplicate this task removes.
	body, err := json.Marshal(SyncTransactionsRequest{Items: []OfflineSyncItem{
		queuedItem("key-modern", ""),
		queuedItem("", "offline-legacy"),
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/sync", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
	}

	var resp SyncAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.WorkflowID != "offline-replay-abc" || resp.ItemCount != 2 {
		t.Fatalf("response = %+v, want the workflow id and both items", resp)
	}

	batch := replay.lastBatch()
	if len(batch) != 2 {
		t.Fatalf("starter received %d items, want 2", len(batch))
	}
	if batch[0].ClientID != "key-modern" {
		t.Fatalf("item 0 key = %q, want key-modern", batch[0].ClientID)
	}
	if batch[1].ClientID != "offline-legacy" {
		t.Fatalf("item 1 key = %q, want the legacy local_id to carry the key", batch[1].ClientID)
	}
	if replay.scopes[0].WorkspaceID != wsID {
		t.Fatalf("scope workspace = %s, want %s", replay.scopes[0].WorkspaceID, wsID)
	}
}

func TestSyncOfflineTransactions_StartFailureIsReported(t *testing.T) {
	_, appPool, _, userID := openTestDB(t)

	r := authedRouter(appPool, &recordingReplay{err: fmt.Errorf("temporal unreachable")}, userID)

	body, err := json.Marshal(SyncTransactionsRequest{Items: []OfflineSyncItem{queuedItem("key-1", "")}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions/sync", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// 502, not 500: the failure is upstream of the API, and the client must keep the batch
	// queued rather than being told the request was malformed.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s, want 502", rec.Code, rec.Body.String())
	}
}
