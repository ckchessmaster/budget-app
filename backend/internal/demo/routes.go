package demo

import (
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
)

// MockBridgeRouter serves the fake SimpleFIN bridge used to simulate bank data for
// demo workspaces.
//
// It is deliberately kept off the public router. Its callers are server-side
// processes holding the per-workspace capability baked into the stored access URL
// (the API itself during /api/v1/bank/simplefin/claim, and the Temporal sync
// worker); neither has an OIDC bearer token, so putting this behind AuthMiddleware
// would break demo bank sync. That stored capability pair is therefore the only
// gate, which is why the bridge never appears on the public ingress. The loopback
// default keeps it reachable from the API process alone; a Helm install with the
// sync worker in its own Deployment binds it routably and exposes it only through
// the cluster-internal backend-bridge Service. Narrowing that further is #129.
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

// RegisterUserRoutes mounts user-scoped demo endpoints (Demo Mode toggle) that are
// available to any authenticated user regardless of active shared-workspace role.
func RegisterUserRoutes(
	r chi.Router,
	pool *pgxpool.Pool,
	mockBaseURL string,
	provisioner workspace.Provisioner,
	vault crypto.Vault,
	temporalClient client.Client,
	reset ResetWorkflow,
) {
	h := NewHandler(pool, vault, mockBaseURL, provisioner, temporalClient, reset)
	r.Post("/api/v1/demo/toggle", h.ToggleDemo)
}

// RegisterWorkspaceRoutes mounts workspace-scoped settings and demo simulator routes
// with RBAC enforcement.
func RegisterWorkspaceRoutes(
	r chi.Router,
	pool *pgxpool.Pool,
	mockBaseURL string,
	provisioner workspace.Provisioner,
	vault crypto.Vault,
	temporalClient client.Client,
	reset ResetWorkflow,
) {
	h := NewHandler(pool, vault, mockBaseURL, provisioner, temporalClient, reset)

	r.Route("/api/v1/settings", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/", h.GetSettings)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Patch("/workspace", h.UpdateWorkspace)
	})

	r.Route("/api/v1/demo", func(r chi.Router) {
		r.Use(middleware.RequireRole(middleware.RoleOwner))
		r.Post("/reset", h.ResetDemo)
		r.Get("/simulator", h.GetSimulatorState)
		r.Post("/simulator/transaction", h.StageTransaction)
		r.Post("/simulator/settle", h.SettlePending)
		r.Post("/simulator/transfer", h.StageTransfer)
		r.Post("/simulator/match-manual", h.StageManualMatch)
		r.Put("/simulator/fault", h.UpdateFault)
	})
}

func RegisterRoutes(
	r chi.Router,
	pool *pgxpool.Pool,
	mockBaseURL string,
	provisioner workspace.Provisioner,
	vault crypto.Vault,
	temporalClient client.Client,
	reset ResetWorkflow,
) {
	RegisterUserRoutes(r, pool, mockBaseURL, provisioner, vault, temporalClient, reset)
	RegisterWorkspaceRoutes(r, pool, mockBaseURL, provisioner, vault, temporalClient, reset)
}
