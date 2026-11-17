package budget

import (
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

	h := NewHandler(pool, catRepo, baselineSvc, reallocSvc, incomeRepo)

	r.Route("/api/v1/categories", func(r chi.Router) {
		r.Get("/", h.ListCategories)
		r.Post("/", h.CreateCategory)
		r.Get("/{id}", h.GetCategory)
		r.Put("/{id}", h.UpdateCategory)
		r.Delete("/{id}", h.DeleteCategory)
	})

	r.Route("/api/v1/budget", func(r chi.Router) {
		r.Get("/", h.GetMonthBudget)
		r.Post("/reallocate", h.ReallocateFunds)
		r.Post("/incomes", h.CreatePlannedIncome)
		r.Get("/incomes", h.ListPlannedIncomes)
		r.Delete("/incomes/{id}", h.DeletePlannedIncome)
	})
}
