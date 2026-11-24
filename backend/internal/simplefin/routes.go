package simplefin

import (
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	client := NewClient(nil)
	vault := crypto.LoadVaultFromEnv()
	h := NewHandler(pool, client, vault)

	r.Route("/api/v1/bank/simplefin", func(r chi.Router) {
		r.Post("/claim", h.ClaimToken)
		r.Post("/link", h.LinkAccounts)
		r.Delete("/", h.Disconnect)
	})
}
