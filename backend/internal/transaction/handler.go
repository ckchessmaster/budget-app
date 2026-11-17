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
	repo TransactionRepo
	svc  TransactionService
}

func NewHandler(pool *pgxpool.Pool, repo TransactionRepo, svc TransactionService) *Handler {
	return &Handler{pool: pool, repo: repo, svc: svc}
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

	var filter TransactionFilter
	q := r.URL.Query()
	if raw := q.Get("account_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_account_id")
			return
		}
		filter.AccountID = &id
	}
	if raw := q.Get("category_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_category_id")
			return
		}
		filter.CategoryID = &id
	}
	if raw := q.Get("status"); raw != "" {
		filter.Status = &raw
	}
	if raw := q.Get("page"); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil {
			filter.Page = p
		}
	}
	if raw := q.Get("limit"); raw != "" {
		if l, err := strconv.Atoi(raw); err == nil {
			filter.Limit = l
		}
	}

	var txs []Transaction
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		txs, txErr = h.repo.List(r.Context(), tx, filter)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_transactions")
		return
	}

	writeJSON(w, http.StatusOK, txs)
}

func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.AccountID == uuid.Nil || req.MerchantRaw == "" || req.TransactionDate == "" {
		writeErr(w, http.StatusBadRequest, "account_id, merchant_raw, and transaction_date are required")
		return
	}

	txDate, err := time.Parse("2006-01-02", req.TransactionDate[:10])
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid transaction_date, expected YYYY-MM-DD")
		return
	}

	if len(req.Splits) > 0 {
		var parent *Transaction
		var children []Transaction
		err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
			var txErr error
			parent, children, txErr = h.svc.CreateSplit(r.Context(), tx, CreateSplitTransactionRequest{
				AccountID:       req.AccountID,
				TotalAmount:     req.Amount,
				MerchantRaw:     req.MerchantRaw,
				TransactionDate: txDate,
				Splits:          req.Splits,
			})
			return txErr
		})
		if err != nil {
			if errors.Is(err, ErrSplitSumMismatch) || errors.Is(err, ErrEmptySplits) {
				writeErr(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			writeErr(w, http.StatusInternalServerError, "failed_to_create_split_transaction")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"parent": parent,
			"splits": children,
		})
		return
	}

	isCleared := true
	if req.IsCleared != nil {
		isCleared = *req.IsCleared
	}
	origin := OriginManualOffline
	if req.Origin != "" {
		origin = TransactionOrigin(req.Origin)
	}
	status := StatusPosted
	if req.Status != "" {
		status = TransactionStatus(req.Status)
	}

	t := &Transaction{
		AccountID:       req.AccountID,
		Origin:          origin,
		Status:          status,
		Amount:          req.Amount.Round(2),
		MerchantRaw:     req.MerchantRaw,
		CategoryID:      req.CategoryID,
		TransactionDate: txDate,
		UserNotes:       req.UserNotes,
		IsCleared:       isCleared,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.svc.CreateSingle(r.Context(), tx, t)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_create_transaction")
		return
	}

	writeJSON(w, http.StatusCreated, t)
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

	var status *TransactionStatus
	if req.Status != nil {
		s := TransactionStatus(*req.Status)
		status = &s
	}

	var updated *Transaction
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		updated, txErr = h.repo.Update(r.Context(), tx, id, req.CategoryID, req.UserNotes, status, req.IsCleared)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
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
		return h.repo.Delete(r.Context(), tx, id)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
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

	results := make([]SyncResultItem, 0, len(req.Transactions))
	hasErrors := false

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		for _, item := range req.Transactions {
			if len(item.TransactionDate) < 10 {
				hasErrors = true
				results = append(results, SyncResultItem{
					LocalID: item.LocalID,
					Status:  "ERROR",
					Error:   "invalid transaction_date",
				})
				continue
			}
			txDate, parseErr := time.Parse("2006-01-02", item.TransactionDate[:10])
			if parseErr != nil {
				hasErrors = true
				results = append(results, SyncResultItem{
					LocalID: item.LocalID,
					Status:  "ERROR",
					Error:   "invalid transaction_date",
				})
				continue
			}

			t := &Transaction{
				AccountID:       item.AccountID,
				Origin:          OriginManualOffline,
				Status:          StatusPosted,
				Amount:          item.Amount.Round(2),
				MerchantRaw:     item.MerchantRaw,
				CategoryID:      item.CategoryID,
				TransactionDate: txDate,
				IsCleared:       true,
			}
			if createErr := h.svc.CreateSingle(r.Context(), tx, t); createErr != nil {
				hasErrors = true
				results = append(results, SyncResultItem{
					LocalID: item.LocalID,
					Status:  "ERROR",
					Error:   createErr.Error(),
				})
				continue
			}
			results = append(results, SyncResultItem{
				LocalID: item.LocalID,
				ID:      t.ID.String(),
				Status:  "SYNCED",
			})
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_sync_transactions")
		return
	}

	statusCode := http.StatusOK
	if hasErrors {
		statusCode = http.StatusMultiStatus
	}
	writeJSON(w, statusCode, map[string]any{"results": results})
}
