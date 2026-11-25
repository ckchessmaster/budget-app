package transaction

import (
	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool, evaluator ...notifications.Evaluator) {
	txRepo := NewRepo()
	acctRepo := account.NewRepo()
	svc := NewService(txRepo, acctRepo)
	h := NewHandler(pool, svc, evaluator...)

	r.Route("/api/v1/transactions", func(r chi.Router) {
		r.Get("/", h.ListTransactions)
		r.Post("/", h.CreateTransaction)
		r.Post("/sync", h.SyncOfflineTransactions)
		r.Patch("/{id}", h.UpdateTransaction)
		r.Delete("/{id}", h.DeleteTransaction)
	})
}
