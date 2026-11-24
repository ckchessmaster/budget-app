package demo

import (
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MockBridgeRouter serves the fake SimpleFIN bridge used to simulate bank data for
// demo workspaces.
//
// It is deliberately kept off the public router. Its callers are server-side
// processes holding the per-workspace capability baked into the stored access URL
// (the API itself during /api/v1/bank/simplefin/claim, and the Temporal sync
// worker); neither has an OIDC bearer token, so putting this behind AuthMiddleware
// would break demo bank sync. Serving it on a loopback-only listener instead keeps
// it reachable to those callers while making it unreachable from other pods.
func MockBridgeRouter(pool *pgxpool.Pool) chi.Router {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	store := NewMockBridgeStore(pool)
	h := NewMockBridgeHandler(store)
	r.Get("/mock-simplefin/{workspace_id}/accounts", h.GetAccounts)
	return r
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
