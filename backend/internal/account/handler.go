package account

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

var ErrLinkedAccountBalanceReadOnly = errors.New("cannot modify balance on a SimpleFIN-linked account; unlink first")

type CreateAccountRequest struct {
	ExternalAccountID    *string         `json:"external_account_id"`
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

type UpdateAccountRequest struct {
	Name                    *string          `json:"name"`
	Type                    *AccountType     `json:"type"`
	CurrentBalance          *decimal.Decimal `json:"current_balance"`
	IsOnBudget              *bool            `json:"is_on_budget"`
	CardMode                *CCMode          `json:"card_mode"`
	InitialDebtBaseline     *decimal.Decimal `json:"initial_debt_baseline"`
	APRPercentage           *decimal.Decimal `json:"apr_percentage"`
	StatementDueDay         *int             `json:"statement_due_day"`
	ClearStatementDueDay    bool             `json:"clear_statement_due_day"`
	DebtPayoffCategoryID    *uuid.UUID       `json:"debt_payoff_category_id"`
	ClearDebtPayoffCategory bool             `json:"clear_debt_payoff_category"`
	ExternalAccountID       *string          `json:"external_account_id"`
	UnlinkExternal          bool             `json:"unlink_external"`
	RecordBalanceAdjustment bool             `json:"record_balance_adjustment"`
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

	isOnBudget := req.Type != Investment && req.Type != Loan

	acct := &Account{
		ExternalAccountID:    req.ExternalAccountID,
		Name:                 strings.TrimSpace(req.Name),
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

// applyAccountUpdate applies partial patch semantics to existing and returns the balance delta.
func applyAccountUpdate(existing *Account, req UpdateAccountRequest) (decimal.Decimal, error) {
	if req.UnlinkExternal {
		existing.ExternalAccountID = nil
	} else if req.ExternalAccountID != nil {
		existing.ExternalAccountID = req.ExternalAccountID
	}

	if req.Name != nil {
		existing.Name = strings.TrimSpace(*req.Name)
	}
	if req.Type != nil {
		existing.Type = *req.Type
	}
	existing.IsOnBudget = existing.Type != Investment && existing.Type != Loan
	if req.CardMode != nil {
		existing.CardMode = *req.CardMode
	}
	if req.InitialDebtBaseline != nil {
		existing.InitialDebtBaseline = req.InitialDebtBaseline.Round(2)
	}
	if req.APRPercentage != nil {
		existing.APRPercentage = req.APRPercentage.Round(2)
	}

	if req.ClearStatementDueDay {
		existing.StatementDueDay = nil
	} else if req.StatementDueDay != nil {
		existing.StatementDueDay = req.StatementDueDay
	}

	if req.ClearDebtPayoffCategory {
		existing.DebtPayoffCategoryID = nil
	} else if req.DebtPayoffCategoryID != nil {
		existing.DebtPayoffCategoryID = req.DebtPayoffCategoryID
	}

	if existing.Type != CreditCard {
		existing.CardMode = PayInFull
		existing.DebtPayoffCategoryID = nil
		existing.StatementDueDay = nil
		existing.InitialDebtBaseline = decimal.Zero
	} else if existing.CardMode == PayInFull {
		existing.DebtPayoffCategoryID = nil
	}

	delta := decimal.Zero
	if req.CurrentBalance != nil {
		oldBal := existing.CurrentBalance.Round(2)
		newBal := req.CurrentBalance.Round(2)
		if existing.ExternalAccountID != nil && !newBal.Equal(oldBal) {
			return decimal.Zero, ErrLinkedAccountBalanceReadOnly
		}
		delta = newBal.Sub(oldBal).Round(2)
		existing.CurrentBalance = newBal
	}

	if err := ValidateAccount(existing); err != nil {
		return decimal.Zero, err
	}
	return delta, nil
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

	var req UpdateAccountRequest
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
		delta, txErr := applyAccountUpdate(existing, req)
		if txErr != nil {
			return txErr
		}
		if txErr := h.svc.UpdateAccount(r.Context(), tx, existing); txErr != nil {
			return txErr
		}
		if req.RecordBalanceAdjustment && !delta.IsZero() && existing.ExternalAccountID == nil {
			if _, txErr := tx.Exec(r.Context(), `
				INSERT INTO transactions (
					id, workspace_id, account_id, origin, status, amount,
					merchant_raw, merchant_clean, category_id, transaction_date, is_cleared
				) VALUES (
					$1, $2, $3, 'MANUAL_OFFLINE', 'POSTED', $4,
					'Balance Adjustment', 'Balance Adjustment', NULL, CURRENT_DATE, TRUE
				)
			`, uuid.New(), existing.WorkspaceID, existing.ID, delta); txErr != nil {
				return txErr
			}
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
		errors.Is(err, ErrInvalidStatementDueDay) ||
		errors.Is(err, ErrLinkedAccountBalanceReadOnly)
}
