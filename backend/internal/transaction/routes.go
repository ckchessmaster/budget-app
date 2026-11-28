package transaction

import (
	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterRoutes mounts the transaction API. replay starts the durable offline sync; the
// server passes a Temporal starter, or nil to make that endpoint report
// temporal_unavailable rather than pretend the batch was queued.
func RegisterRoutes(r chi.Router, pool *pgxpool.Pool, replay ReplayWorkflow, evaluator ...notifications.Evaluator) {
	txRepo := NewRepo()
	acctRepo := account.NewRepo()
	svc := NewService(txRepo, acctRepo)
	h := NewHandler(pool, svc, replay, evaluator...)

	r.Route("/api/v1/transactions", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/", h.ListTransactions)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/", h.CreateTransaction)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/sync", h.SyncOfflineTransactions)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/ignore-review-before", h.IgnoreReviewBefore)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Patch("/{id}", h.UpdateTransaction)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Put("/{id}/splits", h.ReplaceSplits)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Delete("/{id}", h.DeleteTransaction)
	})
}
