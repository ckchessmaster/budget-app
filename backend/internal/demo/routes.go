package demo

import (
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterMockBridgeRoutes(r chi.Router, pool *pgxpool.Pool) {
	store := NewMockBridgeStore(pool)
	h := NewMockBridgeHandler(store)
	r.Get("/mock-simplefin/{workspace_id}/accounts", h.GetAccounts)
}

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool, mockBaseURL string, provisioner workspace.Provisioner) {
	vault := crypto.LoadVaultFromEnv()
	h := NewHandler(pool, vault, mockBaseURL, provisioner)

	r.Route("/api/v1/settings", func(r chi.Router) {
		r.Get("/", h.GetSettings)
		r.Patch("/workspace", h.UpdateWorkspace)
	})

	r.Route("/api/v1/demo", func(r chi.Router) {
		r.Post("/toggle", h.ToggleDemo)
		r.Post("/reset", h.ResetDemo)
		r.Get("/simulator", h.GetSimulatorState)
		r.Post("/simulator/transaction", h.StageTransaction)
		r.Post("/simulator/settle", h.SettlePending)
		r.Post("/simulator/transfer", h.StageTransfer)
		r.Post("/simulator/match-manual", h.StageManualMatch)
		r.Put("/simulator/fault", h.UpdateFault)
	})
}
