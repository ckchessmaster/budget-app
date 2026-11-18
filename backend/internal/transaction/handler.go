package transaction

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool *pgxpool.Pool
	svc  Service
}

func NewHandler(pool *pgxpool.Pool, svc Service) *Handler {
	return &Handler{pool: pool, svc: svc}
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

func (h *Handler) SyncOfflineTransactions(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req SyncTransactionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	results := make([]SyncItemResult, 0, len(req.Items))
	hasFailures := false

	for _, item := range req.Items {
		txDate, parseErr := time.Parse("2006-01-02", item.TransactionDate)
		if parseErr != nil {
			if t2, err2 := time.Parse(time.RFC3339, item.TransactionDate); err2 == nil {
				txDate = t2
			} else {
				txDate = time.Now().UTC()
			}
		}

		isCleared := false
		var createdID uuid.UUID
		txErr := db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
			created, err := h.svc.CreateTransaction(r.Context(), tx, CreateSplitTransactionRequest{
				AccountID:       item.AccountID,
				TotalAmount:     item.Amount,
				MerchantRaw:     item.MerchantRaw,
				CategoryID:      item.CategoryID,
				Origin:          OriginManualOffline,
				Status:          StatusPending,
				TransactionDate: txDate,
				UserNotes:       item.UserNotes,
				IsCleared:       &isCleared,
			})
			if err != nil {
				return err
			}
			createdID = created.ID
			return nil
		})
		if txErr != nil {
			hasFailures = true
			results = append(results, SyncItemResult{
				LocalID: item.LocalID,
				Status:  "failed",
				Error:   txErr.Error(),
			})
		} else {
			idCopy := createdID
			results = append(results, SyncItemResult{
				LocalID:       item.LocalID,
				Status:        "synced",
				TransactionID: &idCopy,
			})
		}
	}

	status := http.StatusOK
	if hasFailures {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, SyncTransactionsResponse{Results: results})
}
