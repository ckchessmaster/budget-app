package admin

import "github.com/go-chi/chi/v5"

func RegisterPublicRoutes(r chi.Router, h *Handler) {
	if h == nil {
		return
	}
	r.Get("/api/v1/signup/validate", h.ValidateSignupInvite)
	r.Post("/api/v1/signup/redeem", h.RedeemSignupInvite)
}

func RegisterAdminRoutes(r chi.Router, h *Handler) {
	if h == nil {
		return
	}
	r.Post("/api/v1/admin/invitations", h.CreateInvitation)
	r.Get("/api/v1/admin/invitations", h.ListInvitations)
	r.Delete("/api/v1/admin/invitations/{id}", h.RevokeInvitation)

	r.Get("/api/v1/admin/users", h.ListUsers)
	r.Patch("/api/v1/admin/users/{userId}/status", h.UpdateUserStatus)
	r.Delete("/api/v1/admin/users/{userId}", h.DeleteUser)

	r.Get("/api/v1/admin/workspaces", h.ListWorkspaces)
	r.Get("/api/v1/admin/health", h.GetHealth)
}
