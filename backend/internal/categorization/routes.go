package categorization

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterRoutes(r chi.Router, pool *pgxpool.Pool) {
	h := NewHandler(pool)
	r.Route("/api/v1/categorization", func(r chi.Router) {
		r.Get("/suggestions", h.GetSuggestions)
		r.Get("/rules", h.ListRules)
		r.Post("/rules", h.CreateRule)
		r.Put("/rules/reorder", h.ReorderRules)
		r.Put("/rules/{id}", h.UpdateRule)
		r.Delete("/rules/{id}", h.DeleteRule)
		r.Post("/rules/{id}/apply", h.ApplyRule)
		r.Post("/apply-similar", h.ApplySimilar)
	})
}
