package budget

import (
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	catRepo := NewCategoryRepo()
	baseRepo := NewBaselineRepo()
	reallocRepo := NewReallocationRepo()
	incomeRepo := NewPlannedIncomeRepo()

	baselineSvc := NewBaselineService(baseRepo)
	reallocSvc := NewReallocationService(catRepo, baseRepo, reallocRepo)
	interlockSvc := NewCCDebtInterlockService()

	h := NewHandler(pool, catRepo, baselineSvc, reallocSvc, incomeRepo, interlockSvc)

	r.Route("/api/v1/categories", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/", h.ListCategories)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/", h.CreateCategory)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/{id}", h.GetCategory)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Put("/{id}", h.UpdateCategory)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Delete("/{id}", h.DeleteCategory)
	})

	r.Route("/api/v1/budget", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/", h.GetMonthBudget)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Put("/categories/{id}", h.UpdateMonthlyPlanned)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/reallocate", h.ReallocateFunds)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/incomes", h.CreatePlannedIncome)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/incomes", h.ListPlannedIncomes)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Patch("/incomes/{id}", h.UpdatePlannedIncome)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Delete("/incomes/{id}", h.DeletePlannedIncome)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/cc-interlocks", h.ListCCDebtInterlocks)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember)).Post("/cc-interlocks/{id}/resolve", h.ResolveCCDebtInterlock)
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/recommendations", h.GetSpendRecommendations)
	})
}
