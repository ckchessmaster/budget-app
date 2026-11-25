package transaction

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool      *pgxpool.Pool
	svc       Service
	evaluator notifications.Evaluator

	// replay starts the durable offline replay. An interface rather than a call into the
	// workflow packages: those import this package's service through the replay activity,
	// so importing them back here would close a cycle. The server wires in the starter.
	replay ReplayWorkflow
}

// ReplayWorkflow submits a queued batch to Temporal and returns the workflow id that will
// carry its verdicts.
type ReplayWorkflow interface {
	Start(ctx context.Context, scope temporalinternal.SyncScope, items []temporalinternal.ReplayItem) (string, error)
}

func NewHandler(pool *pgxpool.Pool, svc Service, replay ReplayWorkflow, evaluator ...notifications.Evaluator) *Handler {
	var eval notifications.Evaluator
	if len(evaluator) > 0 {
		eval = evaluator[0]
	}
	return &Handler{pool: pool, svc: svc, replay: replay, evaluator: eval}
}

func (h *Handler) triggerNotificationEval(ctx context.Context, workspaceID, userID uuid.UUID) {
	if h.evaluator == nil || workspaceID == uuid.Nil || userID == uuid.Nil {
		return
	}
	evalCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := h.evaluator.EvaluateWorkspace(evalCtx, workspaceID, userID); err != nil {
		slog.Warn("post-transaction notification evaluation failed",
			"workspace_id", workspaceID,
			"user_id", userID,
			"error", err)
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

func (h *Handler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := r.URL.Query()
	filter := TransactionFilter{
		Page:  1,
		Limit: 50,
	}

	if rawAcct := q.Get("account_id"); rawAcct != "" {
		id, err := uuid.Parse(rawAcct)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_account_id")
			return
		}
		filter.AccountID = &id
	}
	if rawCat := q.Get("category_id"); rawCat != "" {
		id, err := uuid.Parse(rawCat)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_category_id")
			return
		}
		filter.CategoryID = &id
	}
	if rawStatus := q.Get("status"); rawStatus != "" {
		filter.Status = &rawStatus
	}
	if rawMonth := q.Get("month"); rawMonth != "" {
		parsedMonth, err := time.Parse("2006-01", rawMonth)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_month")
			return
		}
		filter.Month = &parsedMonth
	}
	if rawPage := q.Get("page"); rawPage != "" {
		if p, err := strconv.Atoi(rawPage); err == nil && p > 0 {
			filter.Page = p
		}
	}
	if rawLimit := q.Get("limit"); rawLimit != "" {
		if l, err := strconv.Atoi(rawLimit); err == nil && l > 0 {
			filter.Limit = l
		}
	}

	var items []Transaction
	var total int
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		items, total, txErr = h.svc.ListTransactions(r.Context(), tx, filter)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_transactions")
		return
	}
	if items == nil {
		items = []Transaction{}
	}

	writeJSON(w, http.StatusOK, PaginatedTransactionsResponse{
		Items: items,
		Total: total,
		Page:  filter.Page,
		Limit: filter.Limit,
	})
}

func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateSplitTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var created *Transaction
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		created, txErr = h.svc.CreateTransaction(r.Context(), tx, req)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrSplitSumMismatch) || errors.Is(err, ErrEmptyMerchant) || errors.Is(err, ErrMissingAccountID) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_create_transaction")
		return
	}

	h.triggerNotificationEval(r.Context(), created.WorkspaceID, userID)
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) UpdateTransaction(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_transaction_id")
		return
	}

	var req UpdateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var updated *Transaction
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		updated, txErr = h.svc.UpdateTransaction(r.Context(), tx, id, req)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrTransactionNotFound) {
			writeErr(w, http.StatusNotFound, "transaction_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_update_transaction")
		return
	}

	h.triggerNotificationEval(r.Context(), updated.WorkspaceID, userID)
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteTransaction(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_transaction_id")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.svc.DeleteTransaction(r.Context(), tx, id)
	})
	if err != nil {
		if errors.Is(err, ErrTransactionNotFound) {
			writeErr(w, http.StatusNotFound, "transaction_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_transaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// maxSyncRequestBodyBytes bounds the submitted batch on the way in.
//
// The item cap alone is not a bound: an entry can carry a long notes field. Rejecting an
// absurd body at the edge is cheaper than decoding it, and far cheaper than discovering the
// problem after the batch has been accepted into a workflow.
const maxSyncRequestBodyBytes = 1 << 20

// SyncOfflineTransactions queues a batch of offline-created transactions in Temporal.
//
// It used to apply them in a loop inside the request and return 207 with a per-item verdict.
// That made the browser the durable store for a partially applied batch: close the tab and
// the remainder was gone, and an item lost to a deadlock had to be retried by whoever
// happened to notice. Now the request only has to survive long enough to hand the batch over,
// and the verdicts are read from the workflow.
//
// The response is 202 plus a workflow id. Per-item outcomes come from
// GET /api/v1/transactions/workflow-status/{workflow_id}, which reports the workflow result
// as raw JSON: applied client ids, and rejected ones with a reason and whether they are worth
// sending again.
func (h *Handler) SyncOfflineTransactions(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req SyncTransactionsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSyncRequestBodyBytes))
	if err := decoder.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	if len(req.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "empty_batch")
		return
	}
	if len(req.Items) > temporalinternal.MaxReplayBatchItems {
		// The alternative is a workflow whose input exceeds the message limit, which
		// fails inside the worker where the client cannot see why.
		writeErr(w, http.StatusRequestEntityTooLarge, "batch_too_large")
		return
	}

	items := make([]temporalinternal.ReplayItem, 0, len(req.Items))
	for i, raw := range req.Items {
		item, itemErr := raw.replayItem()
		if itemErr != nil {
			// Nothing in the batch is safe to apply without a key to report its
			// outcome against, so this rejects the request rather than the item.
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": ErrClientKeyRequired.Error(),
				"index": i,
			})
			return
		}
		items = append(items, item)
	}

	if h.replay == nil {
		slog.Error("offline replay requested but no replay workflow is configured")
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

	workflowID, err := h.replay.Start(r.Context(),
		temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}, items)
	if err != nil {
		slog.Error("failed to start offline replay workflow",
			"workspace_id", wsID, "item_count", len(items), "error", err)
		writeErr(w, http.StatusBadGateway, "failed_to_start_offline_replay")
		return
	}

	writeJSON(w, http.StatusAccepted, SyncAcceptedResponse{
		WorkflowID: workflowID,
		Status:     "accepted",
		ItemCount:  len(items),
	})
}
