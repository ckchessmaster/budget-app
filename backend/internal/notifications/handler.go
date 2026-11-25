package notifications

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc PushService
}

func NewHandler(svc PushService) *Handler {
	return &Handler{svc: svc}
}

func RegisterRoutes(r chi.Router, svc PushService) {
	h := NewHandler(svc)
	r.Route("/api/v1/notifications", func(r chi.Router) {
		r.Get("/config", h.GetConfig)
		r.Post("/subscribe", h.Subscribe)
		r.Delete("/subscribe", h.Unsubscribe)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
	Keys     *struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	if _, err := middleware.GetUserID(r.Context()); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":          h.svc.Enabled(),
		"vapid_public_key": h.svc.PublicKey(),
	})
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.svc.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "push_notifications_disabled")
		return
	}

	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	p256dh := strings.TrimSpace(req.P256dh)
	auth := strings.TrimSpace(req.Auth)
	if req.Keys != nil {
		if p256dh == "" {
			p256dh = strings.TrimSpace(req.Keys.P256dh)
		}
		if auth == "" {
			auth = strings.TrimSpace(req.Keys.Auth)
		}
	}

	err = h.svc.Subscribe(r.Context(), userID, PushSubscriptionDTO{
		Endpoint: req.Endpoint,
		P256dh:   p256dh,
		Auth:     auth,
	})
	if err != nil {
		if errors.Is(err, ErrPushDisabled) {
			writeErr(w, http.StatusServiceUnavailable, "push_notifications_disabled")
			return
		}
		if errors.Is(err, ErrInvalidSubscription) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_save_push_subscription")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "subscribed",
	})
}

func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid_json")
			return
		}
	}

	endpoint := strings.TrimSpace(body.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(r.URL.Query().Get("endpoint"))
	}
	if endpoint == "" {
		writeErr(w, http.StatusBadRequest, "endpoint is required")
		return
	}

	if err := h.svc.Unsubscribe(r.Context(), userID, endpoint); err != nil {
		if errors.Is(err, ErrInvalidSubscription) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_push_subscription")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
