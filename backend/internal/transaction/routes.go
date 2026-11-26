package transaction

import (
	"github.com/ckchessmaster/budget-app/backend/internal/account"
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
		r.Get("/", h.ListTransactions)
		r.Post("/", h.CreateTransaction)
		r.Post("/sync", h.SyncOfflineTransactions)
		r.Post("/ignore-review-before", h.IgnoreReviewBefore)
		r.Patch("/{id}", h.UpdateTransaction)
		r.Put("/{id}/splits", h.ReplaceSplits)
		r.Delete("/{id}", h.DeleteTransaction)
	})
}
