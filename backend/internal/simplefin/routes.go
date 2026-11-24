package simplefin

import (
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterRoutes wires the SimpleFIN endpoints. The vault is injected rather than read
// from the environment here so a missing master key stops the process at boot instead of
// surfacing later as an undecryptable stored access URL.
func RegisterRoutes(r chi.Router, pool *pgxpool.Pool, vault crypto.Vault) {
	client := NewClient(nil)
	h := NewHandler(pool, client, vault)

	r.Route("/api/v1/bank/simplefin", func(r chi.Router) {
		r.Post("/claim", h.ClaimToken)
		r.Post("/link", h.LinkAccounts)
		r.Delete("/", h.Disconnect)
	})
}
