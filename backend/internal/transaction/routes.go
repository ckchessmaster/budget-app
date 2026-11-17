package transaction

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	repo := NewTransactionRepo()
	svc := NewTransactionService(repo)
	h := NewHandler(pool, repo, svc)

	r.Route("/api/v1/transactions", func(r chi.Router) {
		r.Get("/", h.ListTransactions)
		r.Post("/", h.CreateTransaction)
		r.Post("/sync", h.SyncOfflineTransactions)
		r.Patch("/{id}", h.UpdateTransaction)
		r.Delete("/{id}", h.DeleteTransaction)
	})
}
