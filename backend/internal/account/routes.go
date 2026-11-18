package account

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	repo := NewRepo()
	svc := NewService(repo)
	h := NewHandler(pool, svc)

	r.Route("/api/v1/accounts", func(r chi.Router) {
		r.Get("/", h.ListAccounts)
		r.Post("/", h.CreateAccount)
		r.Get("/solvency", h.GetSolvencyReport)
		r.Get("/{id}", h.GetAccount)
		r.Patch("/{id}", h.UpdateAccount)
		r.Delete("/{id}", h.DeleteAccount)
		r.Get("/{id}/debt-projection", h.GetDebtProjection)
		r.Post("/{id}/revolver-payment", h.RevolverPaymentSplit)
	})
}
