package household

import (
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
)

func RegisterPublicRoutes(r chi.Router, h *Handler) {
	r.Get("/api/v1/invitations/preview", h.PreviewInvitation)
	r.Post("/api/v1/invitations/accept", h.AcceptInvitation)
}

func RegisterUserRoutes(r chi.Router, h *Handler) {
	r.Get("/api/v1/workspaces", h.ListWorkspaces)
	r.Post("/api/v1/workspaces", h.CreateWorkspace)
}

func RegisterWorkspaceRoutes(r chi.Router, h *Handler) {
	r.Route("/api/v1/workspace", func(r chi.Router) {
		r.With(middleware.RequireRole(middleware.RoleOwner)).Get("/invitations", h.ListInvitations)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Post("/invitations", h.CreateInvitation)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Post("/invitations/{id}/resend", h.ResendInvitation)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Delete("/invitations/{id}", h.RevokeInvitation)

		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Get("/members", h.ListMembers)
		r.With(middleware.RequireRole(middleware.RoleOwner)).Patch("/members/{userId}", h.UpdateMemberRole)
		// Handler enforces self-leave (member/read_only) vs owner-removing-member.
		r.With(middleware.RequireRole(middleware.RoleOwner, middleware.RoleMember, middleware.RoleReadOnly)).Delete("/members/{userId}", h.RemoveMember)
	})
}
