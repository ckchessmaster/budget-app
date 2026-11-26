package budget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type Handler struct {
	pool         *pgxpool.Pool
	catRepo      CategoryRepo
	baselineSvc  BaselineService
	reallocSvc   ReallocationService
	incomeRepo   PlannedIncomeRepo
	interlockSvc CCDebtInterlockService
}

func NewHandler(
	pool *pgxpool.Pool,
	catRepo CategoryRepo,
	baselineSvc BaselineService,
	reallocSvc ReallocationService,
	incomeRepo PlannedIncomeRepo,
	interlockSvc CCDebtInterlockService,
) *Handler {
	if interlockSvc == nil {
		interlockSvc = NewCCDebtInterlockService()
	}
	return &Handler{
		pool:         pool,
		catRepo:      catRepo,
		baselineSvc:  baselineSvc,
		reallocSvc:   reallocSvc,
		incomeRepo:   incomeRepo,
		interlockSvc: interlockSvc,
	}
}

func parseMonthParam(raw string) (time.Time, error) {
	if raw == "" {
		return NormalizeYearMonth(time.Now().UTC()), nil
	}
	if len(raw) == 7 {
		t, err := time.Parse("2006-01", raw)
		if err != nil {
			return time.Time{}, err
		}
		return NormalizeYearMonth(t), nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339, raw)
		if err2 != nil {
			return time.Time{}, err
		}
		return NormalizeYearMonth(t2), nil
	}
	return NormalizeYearMonth(t), nil
}

func parseOptionalDate(raw *string) (*time.Time, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", *raw)
	if err != nil {
		return nil, err
	}
	u := t.UTC()
	return &u, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var categories []Category
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		categories, txErr = h.catRepo.ListByWorkspace(r.Context(), tx)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_categories")
		return
	}

	writeJSON(w, http.StatusOK, categories)
}

func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := req.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	targetDate, err := parseOptionalDate(req.TargetDate)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid target_date format, expected YYYY-MM-DD")
		return
	}

	if req.StartingBalance != nil && req.StartingBalance.LessThan(decimal.Zero) {
		writeErr(w, http.StatusBadRequest, "starting_balance must be >= 0")
		return
	}

	var startYM time.Time
	if req.YearMonth != nil && *req.YearMonth != "" {
		parsedYM, err := time.Parse("2006-01", *req.YearMonth)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid year_month format, expected YYYY-MM")
			return
		}
		startYM = NormalizeYearMonth(parsedYM)
	} else {
		startYM = NormalizeYearMonth(time.Now().UTC())
	}

	alertEnabled, alertThresholdPct := resolveBudgetAlertConfig(req.BudgetAlertEnabled, req.BudgetAlertThresholdPct)
	cat := &Category{
		Name:                    req.Name,
		GroupName:               req.GroupName,
		MonthlyBudget:           req.MonthlyBudget,
		RolloverMode:            RolloverMode(req.RolloverMode),
		SweepTargetCategoryID:   req.SweepTargetCategoryID,
		HeldInAccountID:         req.HeldInAccountID,
		TargetAmount:            req.TargetAmount,
		TargetDate:              targetDate,
		TargetCadence:           req.TargetCadence,
		SortOrder:               req.SortOrder,
		BudgetAlertEnabled:      alertEnabled,
		BudgetAlertThresholdPct: alertThresholdPct,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		wsID, wsErr := db.ActiveWorkspaceID(r.Context(), tx)
		if wsErr != nil {
			return wsErr
		}
		if cat.RolloverMode != RolloverSweep && cat.HeldInAccountID != nil {
			if err := validateHeldInAccount(r.Context(), tx, wsID, *cat.HeldInAccountID); err != nil {
				return err
			}
		}
		if err := h.catRepo.Create(r.Context(), tx, cat); err != nil {
			return err
		}
		if req.StartingBalance != nil && req.StartingBalance.GreaterThanOrEqual(decimal.Zero) && cat.RolloverMode != RolloverSweep {
			_, err := tx.Exec(r.Context(), `
				INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned, carryover_balance)
				VALUES ($1, $2, $3::date, $4, $5)
				ON CONFLICT (workspace_id, category_id, year_month)
				DO UPDATE SET carryover_balance = EXCLUDED.carryover_balance
			`, cat.WorkspaceID, cat.ID, startYM, cat.MonthlyBudget, req.StartingBalance.Round(2))
			if err != nil {
				return fmt.Errorf("failed to set starting balance: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrCyclicSweep) || errors.Is(err, ErrInvalidHeldAccount) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_create_category")
		return
	}

	writeJSON(w, http.StatusCreated, cat)
}

func (h *Handler) GetCategory(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	catID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_category_id")
		return
	}

	var cat *Category
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var txErr error
		cat, txErr = h.catRepo.GetByID(r.Context(), tx, catID)
		return txErr
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "category_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_get_category")
		return
	}

	writeJSON(w, http.StatusOK, cat)
}

func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	catID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_category_id")
		return
	}

	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := req.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	targetDate, err := parseOptionalDate(req.TargetDate)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid target_date format, expected YYYY-MM-DD")
		return
	}

	if req.StartingBalance != nil && req.StartingBalance.LessThan(decimal.Zero) {
		writeErr(w, http.StatusBadRequest, "starting_balance must be >= 0")
		return
	}

	var startYM time.Time
	if req.YearMonth != nil && *req.YearMonth != "" {
		parsedYM, err := time.Parse("2006-01", *req.YearMonth)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid year_month format, expected YYYY-MM")
			return
		}
		startYM = NormalizeYearMonth(parsedYM)
	} else {
		startYM = NormalizeYearMonth(time.Now().UTC())
	}

	alertEnabled, alertThresholdPct := resolveBudgetAlertConfig(req.BudgetAlertEnabled, req.BudgetAlertThresholdPct)
	cat := &Category{
		ID:                      catID,
		Name:                    req.Name,
		GroupName:               req.GroupName,
		MonthlyBudget:           req.MonthlyBudget,
		RolloverMode:            RolloverMode(req.RolloverMode),
		SweepTargetCategoryID:   req.SweepTargetCategoryID,
		HeldInAccountID:         req.HeldInAccountID,
		TargetAmount:            req.TargetAmount,
		TargetDate:              targetDate,
		TargetCadence:           req.TargetCadence,
		SortOrder:               req.SortOrder,
		BudgetAlertEnabled:      alertEnabled,
		BudgetAlertThresholdPct: alertThresholdPct,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		wsID, wsErr := db.ActiveWorkspaceID(r.Context(), tx)
		if wsErr != nil {
			return wsErr
		}
		if cat.RolloverMode != RolloverSweep && cat.HeldInAccountID != nil {
			if err := validateHeldInAccount(r.Context(), tx, wsID, *cat.HeldInAccountID); err != nil {
				return err
			}
		}
		if err := h.catRepo.Update(r.Context(), tx, cat); err != nil {
			return err
		}
		if req.StartingBalance != nil && cat.RolloverMode != RolloverSweep {
			_, err := tx.Exec(r.Context(), `
				INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned, carryover_balance)
				VALUES ($1, $2, $3::date, $4, $5)
				ON CONFLICT (workspace_id, category_id, year_month)
				DO UPDATE SET carryover_balance = EXCLUDED.carryover_balance
			`, wsID, cat.ID, startYM, cat.MonthlyBudget, req.StartingBalance.Round(2))
			if err != nil {
				return fmt.Errorf("failed to update starting balance: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "category_not_found")
			return
		}
		if errors.Is(err, ErrCyclicSweep) || errors.Is(err, ErrInvalidHeldAccount) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_update_category")
		return
	}

	writeJSON(w, http.StatusOK, cat)
}

func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	catID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_category_id")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.catRepo.Delete(r.Context(), tx, catID)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "category_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_category")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetMonthBudget(w http.ResponseWriter, r *http.Request) {
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

	var details *MonthBudgetDetails
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		if err := h.baselineSvc.InitializeMonth(r.Context(), tx, ym); err != nil {
			return err
		}
		var txErr error
		details, txErr = ComputeMonthDetails(r.Context(), tx, ym)
		return txErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_month_budget")
		return
	}

	writeJSON(w, http.StatusOK, details)
}

func (h *Handler) ReallocateFunds(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto ReallocateFundsDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dto.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	ym, err := parseMonthParam(dto.YearMonth)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid year_month format")
		return
	}

	req := ReallocationRequest{
		YearMonth:      ym,
		FromCategoryID: dto.FromCategoryID,
		ToCategoryID:   dto.ToCategoryID,
		Amount:         dto.Amount,
		Reason:         dto.Reason,
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.reallocSvc.TransferFunds(r.Context(), tx, req)
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAmount) || errors.Is(err, ErrSameCategoryTransfer) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "category_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_reallocate_funds")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reallocated"})
}

func (h *Handler) UpdateMonthlyPlanned(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	catID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_category_id")
		return
	}

	var dto UpdateMonthlyPlannedDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := dto.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	ym, err := parseMonthParam(dto.YearMonth)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid year_month format")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return h.baselineSvc.SetMonthlyPlanned(r.Context(), tx, catID, ym, dto.OriginalPlanned)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "category_not_found")
			return
		}
		if errors.Is(err, ErrInvalidAmount) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_update_monthly_planned")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func validateHeldInAccount(ctx context.Context, tx pgx.Tx, wsID, acctID uuid.UUID) error {
	var acctType string
	err := tx.QueryRow(ctx, `
		SELECT type::text
		FROM accounts
		WHERE id = $1 AND workspace_id = $2
	`, acctID, wsID).Scan(&acctType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidHeldAccount
		}
		return fmt.Errorf("failed to validate held account: %w", err)
	}
	switch acctType {
	case "checking", "savings", "cash":
		return nil
	default:
		return ErrInvalidHeldAccount
	}
}
