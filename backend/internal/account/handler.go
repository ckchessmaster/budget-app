package account

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type CreateAccountRequest struct {
	Name                 string          `json:"name"`
	Type                 AccountType     `json:"type"`
	CurrentBalance       decimal.Decimal `json:"current_balance"`
	IsOnBudget           *bool           `json:"is_on_budget"`
	CardMode             CCMode          `json:"card_mode"`
	InitialDebtBaseline  decimal.Decimal `json:"initial_debt_baseline"`
	APRPercentage        decimal.Decimal `json:"apr_percentage"`
	StatementDueDay      *int            `json:"statement_due_day"`
	DebtPayoffCategoryID *uuid.UUID      `json:"debt_payoff_category_id"`
}

type Handler struct {
	pool *pgxpool.Pool
	svc  AccountService
}

func NewHandler(pool *pgxpool.Pool, svc AccountService) *Handler {
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

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var accounts []Account
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		accounts, txErr = h.svc.ListAccounts(r.Context(), tx)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_accounts")
		return
	}

	writeJSON(w, http.StatusOK, accounts)
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}

	isOnBudget := true
	if req.IsOnBudget != nil {
		isOnBudget = *req.IsOnBudget
	}

	acct := &Account{
		Name:                 req.Name,
		Type:                 req.Type,
		CurrentBalance:       req.CurrentBalance,
		IsOnBudget:           isOnBudget,
		CardMode:             req.CardMode,
		InitialDebtBaseline:  req.InitialDebtBaseline,
		APRPercentage:        req.APRPercentage,
		StatementDueDay:      req.StatementDueDay,
		DebtPayoffCategoryID: req.DebtPayoffCategoryID,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.svc.CreateAccount(r.Context(), tx, acct)
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAccountType) || errors.Is(err, ErrInvalidCardMode) || errors.Is(err, ErrMissingDebtCategory) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_create_account")
		return
	}

	writeJSON(w, http.StatusCreated, acct)
}

func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_account_id")
		return
	}

	var req CreateAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	isOnBudget := true
	if req.IsOnBudget != nil {
		isOnBudget = *req.IsOnBudget
	}

	acct := &Account{
		ID:                   id,
		Name:                 req.Name,
		Type:                 req.Type,
		CurrentBalance:       req.CurrentBalance,
		IsOnBudget:           isOnBudget,
		CardMode:             req.CardMode,
		InitialDebtBaseline:  req.InitialDebtBaseline,
		APRPercentage:        req.APRPercentage,
		StatementDueDay:      req.StatementDueDay,
		DebtPayoffCategoryID: req.DebtPayoffCategoryID,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.svc.UpdateAccount(r.Context(), tx, acct)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "account_not_found")
			return
		}
		if errors.Is(err, ErrInvalidAccountType) || errors.Is(err, ErrInvalidCardMode) || errors.Is(err, ErrMissingDebtCategory) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_update_account")
		return
	}

	writeJSON(w, http.StatusOK, acct)
}

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_account_id")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.svc.DeleteAccount(r.Context(), tx, id)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "account_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_account")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetSolvencyReport(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var report *SolvencyReport
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		report, txErr = ComputeSolvencyReport(r.Context(), tx)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_compute_solvency")
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) ProjectDebtPaydown(w http.ResponseWriter, r *http.Request) {
	var input DebtProjectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	res := CalculateDebtProjection(input)
	writeJSON(w, http.StatusOK, res)
}
