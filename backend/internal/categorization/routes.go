package categorization

import (
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	h := NewHandler(pool)
	r.Route("/api/v1/categorization", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/suggestions", h.GetSuggestions)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/rules", h.ListRules)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/rules", h.CreateRule)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Put("/rules/reorder", h.ReorderRules)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Put("/rules/{id}", h.UpdateRule)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Delete("/rules/{id}", h.DeleteRule)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/rules/{id}/apply", h.ApplyRule)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/apply-similar", h.ApplySimilar)
	})
}
