package account

import (
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	repo := NewRepo()
	svc := NewService(repo)
	h := NewHandler(pool, svc)

	r.Route("/api/v1/accounts", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/", h.ListAccounts)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Post("/", h.CreateAccount)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/solvency", h.GetSolvencyReport)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/{id}", h.GetAccount)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Patch("/{id}", h.UpdateAccount)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Delete("/{id}", h.DeleteAccount)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/{id}/debt-projection", h.GetDebtProjection)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/{id}/revolver-payment", h.RevolverPaymentSplit)
	})
}
