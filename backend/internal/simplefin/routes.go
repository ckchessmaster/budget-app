package simplefin

import (
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterRoutes wires the SimpleFIN endpoints. The vault is injected rather than read
// from the environment here so a missing master key stops the process at boot instead of
// surfacing later as an undecryptable stored access URL.
//
// claim starts the durable token exchange and may be nil only on an install that has no
// Temporal; the claim endpoint answers 503 rather than falling back to an inline call.
func RegisterRoutes(r chi.Router, pool *pgxpool.Pool, vault crypto.Vault, claim ClaimWorkflow) {
	h := NewHandler(pool, vault, claim)

	r.Route("/api/v1/bank/simplefin", func(r chi.Router) {
		r.Post("/claim", h.ClaimToken)
		r.Post("/discover", h.DiscoverAccounts)
		r.Post("/link", h.LinkAccounts)
		r.Delete("/", h.Disconnect)
	})
}
