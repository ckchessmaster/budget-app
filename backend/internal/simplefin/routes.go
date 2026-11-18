package simplefin

import (
	"crypto/sha256"
	"log"
	"os"

	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func loadVaultFromEnv() crypto.Vault {
	if b64 := os.Getenv("VAULT_MASTER_KEY"); b64 != "" {
		v, err := crypto.NewAESGCMVaultFromBase64(b64)
		if err == nil {
			return v
		}
		log.Printf("warning: invalid VAULT_MASTER_KEY (%v), falling back to derived key", err)
	}
	// Development fallback derived deterministically when VAULT_MASTER_KEY is unset.
	sum := sha256.Sum256([]byte("budget-app-dev-vault-key-change-in-production"))
	v, _ := crypto.NewAESGCMVault(sum[:])
	return v
}

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	client := NewClient(nil)
	vault := loadVaultFromEnv()
	h := NewHandler(pool, client, vault)

	r.Route("/api/v1/bank/simplefin", func(r chi.Router) {
		r.Post("/claim", h.ClaimToken)
		r.Post("/link", h.LinkAccounts)
	})
}
