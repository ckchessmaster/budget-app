package categorization

import (
	"encoding/json"
	"errors"
	"net/http"
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
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseMonthParam(raw string) (time.Time, error) {
	if raw == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	if len(raw) == 7 {
		t, err := time.Parse("2006-01", raw)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
}

func (h *Handler) GetSuggestions(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	month, err := parseMonthParam(r.URL.Query().Get("month"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_month")
		return
	}

	var txID *uuid.UUID
	if rawTxID := r.URL.Query().Get("transaction_id"); rawTxID != "" {
		parsed, err := uuid.Parse(rawTxID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_transaction_id")
			return
		}
		txID = &parsed
	}

	var suggestions map[uuid.UUID][]Suggestion
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var sErr error
		suggestions, sErr = SuggestForMonth(r.Context(), tx, month, txID)
		return sErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_compute_suggestions")
		return
	}

	strMap := make(map[string][]Suggestion, len(suggestions))
	for k, v := range suggestions {
		strMap[k.String()] = v
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": strMap})
}

func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var rules []Rule
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var lErr error
		rules, lErr = ListRules(r.Context(), tx)
		return lErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_rules")
		return
	}
	if rules == nil {
		rules = []Rule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var created *Rule
	appliedCount := 0
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var cErr error
		created, cErr = CreateRule(r.Context(), tx, req)
		if cErr != nil {
			return cErr
		}
		if req.ApplyNow && created.IsActive {
			appliedCount, cErr = ApplyRuleByID(r.Context(), tx, created.ID)
			if cErr != nil {
				return cErr
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidRule) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_create_rule")
		return
	}

	writeJSON(w, http.StatusCreated, CreateRuleResponse{
		Rule:         *created,
		AppliedCount: appliedCount,
	})
}

func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_rule_id")
		return
	}

	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var updated *Rule
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var uErr error
		updated, uErr = UpdateRule(r.Context(), tx, id, req)
		return uErr
	})
	if err != nil {
		if errors.Is(err, ErrRuleNotFound) {
			writeErr(w, http.StatusNotFound, "rule_not_found")
			return
		}
		if errors.Is(err, ErrInvalidRule) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_update_rule")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_rule_id")
		return
	}

	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		return DeleteRule(r.Context(), tx, id)
	})
	if err != nil {
		if errors.Is(err, ErrRuleNotFound) {
			writeErr(w, http.StatusNotFound, "rule_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReorderRules(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ReorderRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var rules []Rule
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var rErr error
		rules, rErr = ReorderRules(r.Context(), tx, req.RuleIDs)
		return rErr
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_reorder_rules")
		return
	}
	if rules == nil {
		rules = []Rule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (h *Handler) ApplyRule(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_rule_id")
		return
	}

	var count int
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var aErr error
		count, aErr = ApplyRuleByID(r.Context(), tx, id)
		return aErr
	})
	if err != nil {
		if errors.Is(err, ErrRuleNotFound) {
			writeErr(w, http.StatusNotFound, "rule_not_found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_apply_rule")
		return
	}
	writeJSON(w, http.StatusOK, ApplyResult{UpdatedCount: count})
}

func (h *Handler) ApplySimilar(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ApplySimilarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	var count int
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		var aErr error
		count, aErr = ApplySimilar(r.Context(), tx, req)
		return aErr
	})
	if err != nil {
		if errors.Is(err, ErrInvalidApplyRequest) {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_apply_similar")
		return
	}
	writeJSON(w, http.StatusOK, ApplyResult{UpdatedCount: count})
}
