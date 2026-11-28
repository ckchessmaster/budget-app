package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/idp"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultInviteTTLHours = 168
	maxInviteTTLHours     = 720
)

// PlatformInviteEmailWorkflow starts SendPlatformInviteWorkflow without importing the
// temporal workflows package directly.
type PlatformInviteEmailWorkflow interface {
	Start(ctx context.Context, invitationID uuid.UUID) (string, error)
}

// TemporalHealthChecker checks Temporal reachability for /api/v1/admin/health.
type TemporalHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

type Handler struct {
	pool           *pgxpool.Pool
	vault          crypto.Vault
	idpAdmin       idp.PlatformIdentityAdmin
	inviteWorkflow PlatformInviteEmailWorkflow
	visibility     WorkflowVisibilityClient
	temporalHealth TemporalHealthChecker
	appPublicURL   string
}

func NewHandler(
	pool *pgxpool.Pool,
	vault crypto.Vault,
	idpAdmin idp.PlatformIdentityAdmin,
	inviteWorkflow PlatformInviteEmailWorkflow,
	visibility WorkflowVisibilityClient,
	temporalHealth TemporalHealthChecker,
	appPublicURL string,
) *Handler {
	if idpAdmin == nil {
		idpAdmin = &idp.NoopIdentityProvisioner{AppPublicURL: appPublicURL}
	}
	trimmedURL := strings.TrimRight(strings.TrimSpace(appPublicURL), "/")
	if trimmedURL == "" {
		trimmedURL = "http://localhost:5173"
	}
	return &Handler{
		pool:           pool,
		vault:          vault,
		idpAdmin:       idpAdmin,
		inviteWorkflow: inviteWorkflow,
		visibility:     visibility,
		temporalHealth: temporalHealth,
		appPublicURL:   trimmedURL,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func generatePlatformInviteToken() (rawToken string, tokenHash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	rawToken = base64.RawURLEncoding.EncodeToString(buf)
	return rawToken, hashInviteToken(rawToken), nil
}

func hashInviteToken(rawToken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawToken)))
	return hex.EncodeToString(sum[:])
}

func effectiveInviteStatus(status string, expiresAt time.Time, now time.Time) string {
	if status == "ACTIVE" && !expiresAt.After(now) {
		return "EXPIRED"
	}
	return status
}

func isValidEmail(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 255 {
		return false
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr == nil {
		return false
	}
	parts := strings.Split(addr.Address, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}

type PlatformInvitationDTO struct {
	ID              uuid.UUID  `json:"id"`
	Label           string     `json:"label"`
	RecipientEmail  string     `json:"recipient_email,omitempty"`
	Status          string     `json:"status"`
	ExpiresAt       time.Time  `json:"expires_at"`
	RedeemedByEmail string     `json:"redeemed_by_email,omitempty"`
	RedeemedAt      *time.Time `json:"redeemed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	InviteURL       string     `json:"invite_url,omitempty"`
	EmailDispatched bool       `json:"email_dispatched,omitempty"`
}

type PlatformUserDTO struct {
	UserID               uuid.UUID  `json:"user_id"`
	Email                string     `json:"email"`
	DisplayName          string     `json:"display_name"`
	IsActive             bool       `json:"is_active"`
	LastLoginAt          *time.Time `json:"last_login_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	OwnedWorkspaceCount  int        `json:"owned_workspace_count"`
	MemberWorkspaceCount int        `json:"member_workspace_count"`
}

type AdminWorkspaceOverviewDTO struct {
	WorkspaceID           uuid.UUID  `json:"workspace_id"`
	WorkspaceName         string     `json:"workspace_name"`
	CreatedAt             time.Time  `json:"created_at"`
	MemberCount           int        `json:"member_count"`
	OwnerEmail            string     `json:"owner_email"`
	SimpleFINLastSyncedAt *time.Time `json:"simplefin_last_synced_at,omitempty"`
	SimpleFINStatus       string     `json:"simplefin_status"`
	SimpleFINError        *string    `json:"simplefin_error,omitempty"`
}

// CreateInvitation generates a single-use platform invite token, stores only its
// SHA-256 hash, optionally seals & sends it via Temporal, and returns the one-time URL.
func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	callerID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Label          string `json:"label"`
		ExpiresInHours int    `json:"expires_in_hours"`
		Email          string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	label := strings.TrimSpace(req.Label)
	if len(label) > 120 {
		writeErr(w, http.StatusBadRequest, "label_too_long")
		return
	}

	ttlHours := req.ExpiresInHours
	if ttlHours == 0 {
		ttlHours = defaultInviteTTLHours
	}
	if ttlHours < 1 || ttlHours > maxInviteTTLHours {
		writeErr(w, http.StatusBadRequest, "invalid_expiration")
		return
	}

	recipientEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if recipientEmail != "" && !isValidEmail(recipientEmail) {
		writeErr(w, http.StatusBadRequest, "invalid_email")
		return
	}

	rawToken, tokenHash, err := generatePlatformInviteToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_generate_token")
		return
	}

	var sealedToken *string
	if recipientEmail != "" && h.vault != nil {
		enc, encErr := h.vault.Encrypt(rawToken)
		if encErr != nil {
			writeErr(w, http.StatusInternalServerError, "failed_to_seal_invitation_token")
			return
		}
		sealedToken = &enc
	}

	expiresAt := time.Now().UTC().Add(time.Duration(ttlHours) * time.Hour)

	var out PlatformInvitationDTO
	err = db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			INSERT INTO platform_invitations (
				label, recipient_email, token_hash, encrypted_pending_token,
				status, created_by_user_id, expires_at
			)
			VALUES ($1, $2, $3, $4, 'ACTIVE', $5, $6)
			RETURNING id, label, recipient_email, status::text, expires_at, created_at
		`, label, recipientEmail, tokenHash, sealedToken, callerID, expiresAt).Scan(
			&out.ID, &out.Label, &out.RecipientEmail, &out.Status, &out.ExpiresAt, &out.CreatedAt,
		)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}

	out.InviteURL = h.appPublicURL + "/signup?invite=" + url.QueryEscape(rawToken)

	if recipientEmail != "" && h.inviteWorkflow != nil {
		if _, wfErr := h.inviteWorkflow.Start(r.Context(), out.ID); wfErr != nil {
			slog.Warn("failed to start platform invite email workflow", "invitation_id", out.ID, "error", wfErr)
		} else {
			out.EmailDispatched = true
		}
	}

	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	var list []PlatformInvitationDTO
	now := time.Now().UTC()

	err := db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT pi.id, pi.label, pi.recipient_email, pi.status::text, pi.expires_at,
			       COALESCE(pu.email, ''), pi.redeemed_at, pi.created_at
			FROM platform_invitations pi
			LEFT JOIN platform_users pu ON pu.user_id = pi.redeemed_by_user_id
			ORDER BY pi.created_at DESC
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var item PlatformInvitationDTO
			if err := rows.Scan(
				&item.ID, &item.Label, &item.RecipientEmail, &item.Status,
				&item.ExpiresAt, &item.RedeemedByEmail, &item.RedeemedAt, &item.CreatedAt,
			); err != nil {
				return err
			}
			item.Status = effectiveInviteStatus(item.Status, item.ExpiresAt, now)
			list = append(list, item)
		}
		return rows.Err()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_invitations")
		return
	}
	if list == nil {
		list = []PlatformInvitationDTO{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	invID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_invitation_id")
		return
	}

	var notFound, alreadyRedeemed bool
	err = db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(r.Context(), `
			SELECT status::text FROM platform_invitations WHERE id = $1
		`, invID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				notFound = true
				return nil
			}
			return err
		}
		if status == "REDEEMED" {
			alreadyRedeemed = true
			return nil
		}
		if status == "REVOKED" {
			return nil
		}
		_, err := tx.Exec(r.Context(), `
			UPDATE platform_invitations
			SET status = 'REVOKED', encrypted_pending_token = NULL
			WHERE id = $1
		`, invID)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_revoke_invitation")
		return
	}
	if notFound {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	if alreadyRedeemed {
		writeErr(w, http.StatusConflict, "invitation_already_redeemed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	var users []PlatformUserDTO
	err := db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT user_id, email, display_name, is_active, last_login_at, created_at,
			       owned_workspace_count, member_workspace_count
			FROM admin_list_users()
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var u PlatformUserDTO
			if err := rows.Scan(
				&u.UserID, &u.Email, &u.DisplayName, &u.IsActive,
				&u.LastLoginAt, &u.CreatedAt, &u.OwnedWorkspaceCount, &u.MemberWorkspaceCount,
			); err != nil {
				return err
			}
			users = append(users, u)
		}
		return rows.Err()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_users")
		return
	}
	if users == nil {
		users = []PlatformUserDTO{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	callerID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_user_id")
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IsActive == nil {
		writeErr(w, http.StatusBadRequest, "is_active_required")
		return
	}

	if targetID == callerID && !*req.IsActive {
		writeErr(w, http.StatusBadRequest, "cannot_disable_self")
		return
	}

	var exists bool
	err = db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			SELECT EXISTS(SELECT 1 FROM platform_users WHERE user_id = $1)
		`, targetID).Scan(&exists)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_lookup_user")
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "user_not_found")
		return
	}

	if err := h.idpAdmin.SetUserActive(r.Context(), targetID, *req.IsActive); err != nil {
		if errors.Is(err, idp.ErrUserNotFound) {
			writeErr(w, http.StatusNotFound, "user_not_found")
			return
		}
		writeErr(w, http.StatusBadGateway, "identity_provider_error")
		return
	}

	var updated PlatformUserDTO
	err = db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			UPDATE platform_users
			SET is_active = $2
			WHERE user_id = $1
			RETURNING user_id, email, display_name, is_active, last_login_at, created_at
		`, targetID, *req.IsActive).Scan(
			&updated.UserID, &updated.Email, &updated.DisplayName,
			&updated.IsActive, &updated.LastLoginAt, &updated.CreatedAt,
		)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_user_status")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	callerID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_user_id")
		return
	}

	if targetID == callerID {
		writeErr(w, http.StatusBadRequest, "cannot_delete_self")
		return
	}

	var exists bool
	err = db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			SELECT EXISTS(SELECT 1 FROM platform_users WHERE user_id = $1)
		`, targetID).Scan(&exists)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_lookup_user")
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "user_not_found")
		return
	}

	if err := h.idpAdmin.DeleteUser(r.Context(), targetID); err != nil {
		writeErr(w, http.StatusBadGateway, "identity_provider_error")
		return
	}

	var deleted bool
	err = db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT admin_delete_user($1)`, targetID).Scan(&deleted)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_delete_user")
		return
	}
	if !deleted {
		writeErr(w, http.StatusNotFound, "user_not_found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	var workspaces []AdminWorkspaceOverviewDTO
	err := db.WithPlatformAdminTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT workspace_id, workspace_name, created_at, member_count,
			       COALESCE(owner_email, ''), simplefin_last_synced_at, simplefin_status, simplefin_error
			FROM admin_workspace_overview
			ORDER BY created_at DESC
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var ws AdminWorkspaceOverviewDTO
			if err := rows.Scan(
				&ws.WorkspaceID, &ws.WorkspaceName, &ws.CreatedAt, &ws.MemberCount,
				&ws.OwnerEmail, &ws.SimpleFINLastSyncedAt, &ws.SimpleFINStatus, &ws.SimpleFINError,
			); err != nil {
				return err
			}
			workspaces = append(workspaces, ws)
		}
		return rows.Err()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_workspaces")
		return
	}
	if workspaces == nil {
		workspaces = []AdminWorkspaceOverviewDTO{}
	}
	writeJSON(w, http.StatusOK, workspaces)
}
