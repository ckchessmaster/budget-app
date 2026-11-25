package budget

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) CreatePlannedIncome(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreatePlannedIncomeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := req.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	ym, err := parseMonthParam(req.YearMonth)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid year_month format")
		return
	}

	inc := &MonthlyPlannedIncome{
		YearMonth: ym,
		Name:      req.Name,
		Amount:    req.Amount,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.incomeRepo.Create(r.Context(), tx, inc)
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAmount) || errors.Is(err, ErrNegativeAmount) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_create_planned_income")
		return
	}

	writeJSON(w, http.StatusCreated, inc)
}

func (h *Handler) ListPlannedIncomes(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ym, err := parseMonthParam(r.URL.Query().Get("month"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid month format, expected YYYY-MM")
		return
	}

	var incomes []MonthlyPlannedIncome
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		incomes, txErr = h.incomeRepo.ListByMonth(r.Context(), tx, ym)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_planned_incomes")
		return
	}

	writeJSON(w, http.StatusOK, incomes)
}

func (h *Handler) DeletePlannedIncome(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_income_id")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.incomeRepo.Delete(r.Context(), tx, id)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "planned_income_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_planned_income")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdatePlannedIncome(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_income_id")
		return
	}

	var req struct {
		Name   string          `json:"name"`
		Amount decimal.Decimal `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request_body")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name_required")
		return
	}
	if req.Amount.LessThan(decimal.Zero) {
		writeErr(w, http.StatusBadRequest, "negative_amount")
		return
	}

	var updated *MonthlyPlannedIncome
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		updated, txErr = h.incomeRepo.Update(r.Context(), tx, id, req.Name, req.Amount)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "planned_income_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_update_planned_income")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}
