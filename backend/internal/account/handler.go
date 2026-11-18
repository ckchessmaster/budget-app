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

type CreateAccountRequest struct {
	ExternalAccountID    *string          `json:"external_account_id"`
	Name                 string           `json:"name"`
	Type                 AccountType      `json:"type"`
	CurrentBalance       decimal.Decimal  `json:"current_balance"`
	IsOnBudget           *bool            `json:"is_on_budget"`
	CardMode             CCMode           `json:"card_mode"`
	InitialDebtBaseline  decimal.Decimal  `json:"initial_debt_baseline"`
	APRPercentage        decimal.Decimal  `json:"apr_percentage"`
	StatementDueDay      *int             `json:"statement_due_day"`
	DebtPayoffCategoryID *uuid.UUID       `json:"debt_payoff_category_id"`
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
	if accounts == nil {
		accounts = []Account{}
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

	isOnBudget := true
	if req.IsOnBudget != nil {
		isOnBudget = *req.IsOnBudget
	}

	acct := &Account{
		ExternalAccountID:    req.ExternalAccountID,
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
		if isValidationErr(err) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_create_account")
		return
	}

	writeJSON(w, http.StatusCreated, acct)
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
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

	var acct *Account
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		acct, txErr = h.svc.GetAccount(r.Context(), tx, id)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			writeErr(w, http.StatusNotFound, "account_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_get_account")
		return
	}

	writeJSON(w, http.StatusOK, acct)
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

	var acct *Account
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		existing, txErr := h.svc.GetAccount(r.Context(), tx, id)
		if txErr != nil {
			return txErr
		}
		if req.Name != "" {
			existing.Name = req.Name
		}
		if req.Type != "" {
			existing.Type = req.Type
		}
		if req.IsOnBudget != nil {
			existing.IsOnBudget = *req.IsOnBudget
		}
		if req.CardMode != "" {
			existing.CardMode = req.CardMode
		}
		if req.ExternalAccountID != nil {
			existing.ExternalAccountID = req.ExternalAccountID
		}
		if req.StatementDueDay != nil {
			existing.StatementDueDay = req.StatementDueDay
		}
		if req.DebtPayoffCategoryID != nil {
			existing.DebtPayoffCategoryID = req.DebtPayoffCategoryID
		}
		if !req.CurrentBalance.IsZero() {
			existing.CurrentBalance = req.CurrentBalance
		}
		if !req.InitialDebtBaseline.IsZero() {
			existing.InitialDebtBaseline = req.InitialDebtBaseline
		}
		if !req.APRPercentage.IsZero() {
			existing.APRPercentage = req.APRPercentage
		}
		if txErr := h.svc.UpdateAccount(r.Context(), tx, existing); txErr != nil {
			return txErr
		}
		acct = existing
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			writeErr(w, http.StatusNotFound, "account_not_found")
			return
		}
		if isValidationErr(err) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
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
		if errors.Is(err, ErrAccountNotFound) {
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

func (h *Handler) GetDebtProjection(w http.ResponseWriter, r *http.Request) {
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

	allocStr := r.URL.Query().Get("monthly_allocation")
	alloc, err := decimal.NewFromString(allocStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_monthly_allocation")
		return
	}

	var acct *Account
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		acct, txErr = h.svc.GetAccount(r.Context(), tx, id)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			writeErr(w, http.StatusNotFound, "account_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_get_account")
		return
	}

	res := CalculateDebtProjection(DebtProjectionInput{
		CurrentBalance:    acct.CurrentBalance,
		MonthlyAllocation: alloc,
		APRPercentage:     acct.APRPercentage,
	})
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) RevolverPaymentSplit(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		PaymentAmount decimal.Decimal `json:"payment_amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var res *PaymentSplitResult
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		acct, txErr := h.svc.GetAccount(r.Context(), tx, id)
		if txErr != nil {
			return txErr
		}
		res, txErr = ProcessRevolverPayment(r.Context(), tx, *acct, body.PaymentAmount)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			writeErr(w, http.StatusNotFound, "account_not_found")
			return
		}
		if errors.Is(err, ErrNotDebtPaydownCard) || errors.Is(err, ErrMissingDebtPayoffCat) || errors.Is(err, ErrInvalidPaymentAmount) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_process_revolver_payment")
		return
	}

	writeJSON(w, http.StatusOK, res)
}

func isValidationErr(err error) bool {
	return errors.Is(err, ErrEmptyAccountName) ||
		errors.Is(err, ErrInvalidAccountType) ||
		errors.Is(err, ErrInvalidCardMode) ||
		errors.Is(err, ErrDebtPaydownNonCreditCard) ||
		errors.Is(err, ErrMissingDebtPayoffCat) ||
		errors.Is(err, ErrInvalidStatementDueDay)
}
