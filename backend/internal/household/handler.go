package household

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/idp"
	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const inviteTTL = 7 * 24 * time.Hour

// InviteEmailWorkflow starts SendWorkspaceInviteWorkflow without importing the workflows
// package directly.
type InviteEmailWorkflow interface {
	Start(ctx context.Context, scope temporalinternal.SyncScope, invitationID uuid.UUID) (string, error)
}

// RolloverScheduler ensures month-end rollover schedules exist for newly created workspaces.
type RolloverScheduler interface {
	EnsureWorkspaceRolloverSchedule(ctx context.Context, scope temporalinternal.SyncScope) error
}

type Handler struct {
	pool              *pgxpool.Pool
	vault             crypto.Vault
	provisioner       *workspace.DefaultProvisioner
	idpProvisioner    idp.IdentityProvisioner
	verifier          middleware.OIDCVerifier
	inviteWorkflow    InviteEmailWorkflow
	rolloverScheduler RolloverScheduler
	appPublicURL      string
}

func NewHandler(
	pool *pgxpool.Pool,
	vault crypto.Vault,
	provisioner *workspace.DefaultProvisioner,
	idpProvisioner idp.IdentityProvisioner,
	verifier middleware.OIDCVerifier,
	inviteWorkflow InviteEmailWorkflow,
	rolloverScheduler RolloverScheduler,
	appPublicURL string,
) *Handler {
	if provisioner == nil {
		provisioner = workspace.NewProvisioner()
	}
	if idpProvisioner == nil {
		idpProvisioner = &idp.NoopIdentityProvisioner{}
	}
	if strings.TrimSpace(appPublicURL) == "" {
		appPublicURL = "http://localhost:5173"
	}
	return &Handler{
		pool:              pool,
		vault:             vault,
		provisioner:       provisioner,
		idpProvisioner:    idpProvisioner,
		verifier:          verifier,
		inviteWorkflow:    inviteWorkflow,
		rolloverScheduler: rolloverScheduler,
		appPublicURL:      strings.TrimRight(strings.TrimSpace(appPublicURL), "/"),
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

type InvitationDTO struct {
	ID             uuid.UUID `json:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	InvitedByEmail string    `json:"invited_by_email,omitempty"`
}

type MemberDTO struct {
	UserID        uuid.UUID `json:"user_id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	Role          string    `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
	IsCurrentUser bool      `json:"is_current_user"`
}

type InvitePreviewDTO struct {
	WorkspaceName  string    `json:"workspace_name"`
	Email          string    `json:"email"`
	InvitedByEmail string    `json:"invited_by_email,omitempty"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	ExpiresAt      time.Time `json:"expires_at"`
	EmailMismatch  bool      `json:"email_mismatch"`
	SignedInEmail  string    `json:"signed_in_email,omitempty"`
}

type AcceptInviteResponse struct {
	WorkspaceID   uuid.UUID `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	Role          string    `json:"role"`
	IsNewUser     bool      `json:"is_new_user"`
	RedirectURL   string    `json:"redirect_url"`
}

func generateInviteToken() (rawToken string, tokenHash string, err error) {
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
	if status == "PENDING" && !expiresAt.After(now) {
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
	return strings.EqualFold(addr.Address, trimmed) && strings.Contains(addr.Address, "@")
}

// ListWorkspaces returns all workspaces visible to the authenticated user.
func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var list []workspace.WorkspaceSummary
	err = db.WithWorkspaceTx(r.Context(), h.pool, userID, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(r.Context(), `
			SELECT w.id, w.name, wm.role, w.is_demo
			FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE wm.user_id = $1
			ORDER BY (wm.role = 'owner') DESC, w.created_at ASC
		`, userID)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()

		for rows.Next() {
			var item workspace.WorkspaceSummary
			if scanErr := rows.Scan(&item.ID, &item.Name, &item.Role, &item.IsDemo); scanErr != nil {
				return scanErr
			}
			list = append(list, item)
		}
		return rows.Err()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_workspaces")
		return
	}

	if len(list) == 0 {
		prov, pErr := h.provisioner.EnsureWorkspaceProvisioned(r.Context(), h.pool, userID)
		if pErr != nil {
			writeErr(w, http.StatusInternalServerError, "failed_to_provision_workspace")
			return
		}
		list = []workspace.WorkspaceSummary{{
			ID:     prov.WorkspaceID,
			Name:   prov.WorkspaceName,
			Role:   prov.Role,
			IsDemo: prov.IsDemo,
		}}
	}

	writeJSON(w, http.StatusOK, list)
}

// CreateWorkspace lets a user who first joined a household via invite create their own
// single personal workspace (enforced by idx_unique_owner_per_user).
func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	ws, err := h.provisioner.CreatePersonalWorkspace(
		r.Context(),
		h.pool,
		userID,
		body.Name,
		middleware.GetUserEmail(r.Context()),
		middleware.GetUserName(r.Context()),
	)
	if err != nil {
		switch {
		case errors.Is(err, workspace.ErrInvalidWorkspaceName):
			writeErr(w, http.StatusBadRequest, "workspace name must be 1 to 255 characters")
		case errors.Is(err, workspace.ErrAlreadyOwnsWorkspace):
			writeErr(w, http.StatusConflict, "already_owns_workspace")
		default:
			writeErr(w, http.StatusInternalServerError, "failed_to_create_workspace")
		}
		return
	}

	if h.rolloverScheduler != nil {
		_ = h.rolloverScheduler.EnsureWorkspaceRolloverSchedule(r.Context(), temporalinternal.SyncScope{
			WorkspaceID: ws.ID,
			UserID:      userID,
		})
	}

	writeJSON(w, http.StatusCreated, ws)
}

// ListInvitations returns pending (and expired pending) invitations for the active workspace.
func (h *Handler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	now := time.Now().UTC()
	out := make([]InvitationDTO, 0)
	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(r.Context(), `
			SELECT id, workspace_id, email, role, status::text, expires_at, created_at, invited_by_email
			FROM workspace_invitations
			WHERE workspace_id = $1 AND status = 'PENDING'
			ORDER BY created_at DESC
		`, wc.WorkspaceID)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()

		for rows.Next() {
			var dto InvitationDTO
			if scanErr := rows.Scan(
				&dto.ID, &dto.WorkspaceID, &dto.Email, &dto.Role,
				&dto.Status, &dto.ExpiresAt, &dto.CreatedAt, &dto.InvitedByEmail,
			); scanErr != nil {
				return scanErr
			}
			dto.Status = effectiveInviteStatus(dto.Status, dto.ExpiresAt, now)
			out = append(out, dto)
		}
		return rows.Err()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_invitations")
		return
	}

	writeJSON(w, http.StatusOK, out)
}

// CreateInvitation creates a pending invitation, stages the vault-sealed token, and
// triggers SendWorkspaceInviteWorkflow on Temporal.
func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	if wc.IsDemo {
		writeErr(w, http.StatusBadRequest, "cannot_invite_to_demo_workspace")
		return
	}
	if h.vault == nil {
		writeErr(w, http.StatusServiceUnavailable, "vault_unavailable")
		return
	}
	if h.inviteWorkflow == nil {
		writeErr(w, http.StatusServiceUnavailable, "temporal_unavailable")
		return
	}

	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}

	emailAddr := strings.TrimSpace(req.Email)
	if !isValidEmail(emailAddr) {
		writeErr(w, http.StatusBadRequest, "invalid_email")
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = string(middleware.RoleMember)
	}
	if role != string(middleware.RoleMember) && role != string(middleware.RoleReadOnly) {
		writeErr(w, http.StatusBadRequest, "invalid_role")
		return
	}

	rawToken, tokenHash, err := generateInviteToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_generate_token")
		return
	}
	sealedToken, err := h.vault.Encrypt(rawToken)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_seal_token")
		return
	}

	inviterEmail := middleware.GetUserEmail(r.Context())
	expiresAt := time.Now().UTC().Add(inviteTTL)
	var dto InvitationDTO
	var conflictErr string

	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		var alreadyMember bool
		if err := tx.QueryRow(r.Context(), `
			SELECT EXISTS(
				SELECT 1 FROM workspace_roster
				WHERE workspace_id = $1 AND LOWER(email) = LOWER($2) AND email <> ''
			)
		`, wc.WorkspaceID, emailAddr).Scan(&alreadyMember); err != nil {
			return err
		}
		if alreadyMember {
			conflictErr = "already_member"
			return nil
		}

		// Expire any stale pending invite so re-inviting after 7 days succeeds cleanly.
		if _, err := tx.Exec(r.Context(), `
			UPDATE workspace_invitations
			SET status = 'EXPIRED', encrypted_pending_token = NULL
			WHERE workspace_id = $1 AND LOWER(email) = LOWER($2) AND status = 'PENDING' AND expires_at <= NOW()
		`, wc.WorkspaceID, emailAddr); err != nil {
			return err
		}

		var wsName string
		if err := tx.QueryRow(r.Context(), `SELECT name FROM workspaces WHERE id = $1`, wc.WorkspaceID).Scan(&wsName); err != nil {
			return err
		}

		err := tx.QueryRow(r.Context(), `
			INSERT INTO workspace_invitations (
				workspace_id, workspace_name, invited_by_user_id, invited_by_email,
				email, role, token_hash, encrypted_pending_token, status, expires_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'PENDING', $9)
			RETURNING id, workspace_id, email, role, status::text, expires_at, created_at, invited_by_email
		`, wc.WorkspaceID, wsName, wc.UserID, inviterEmail, emailAddr, role, tokenHash, sealedToken, expiresAt).Scan(
			&dto.ID, &dto.WorkspaceID, &dto.Email, &dto.Role,
			&dto.Status, &dto.ExpiresAt, &dto.CreatedAt, &dto.InvitedByEmail,
		)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				conflictErr = "invite_already_pending"
				return nil
			}
			return err
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_create_invitation")
		return
	}
	if conflictErr != "" {
		writeErr(w, http.StatusConflict, conflictErr)
		return
	}

	if _, err := h.inviteWorkflow.Start(r.Context(), temporalinternal.SyncScope{
		WorkspaceID: wc.WorkspaceID,
		UserID:      wc.UserID,
	}, dto.ID); err != nil {
		// Clean up the persisted invite if Temporal could not accept the workflow.
		_ = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
			_, _ = tx.Exec(r.Context(), `DELETE FROM workspace_invitations WHERE id = $1`, dto.ID)
			return nil
		})
		writeErr(w, http.StatusInternalServerError, "failed_to_dispatch_invitation")
		return
	}

	writeJSON(w, http.StatusCreated, dto)
}

// ResendInvitation rotates the cryptographic token hash, resets the 7-day expiration,
// and dispatches a fresh SendWorkspaceInviteWorkflow.
func (h *Handler) ResendInvitation(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}
	if h.vault == nil {
		writeErr(w, http.StatusServiceUnavailable, "vault_unavailable")
		return
	}
	if h.inviteWorkflow == nil {
		writeErr(w, http.StatusServiceUnavailable, "temporal_unavailable")
		return
	}

	invID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_invitation_id")
		return
	}

	rawToken, tokenHash, err := generateInviteToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_generate_token")
		return
	}
	sealedToken, err := h.vault.Encrypt(rawToken)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_seal_token")
		return
	}

	newExpiry := time.Now().UTC().Add(inviteTTL)
	var dto InvitationDTO
	var notFound, notPending bool

	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		var currentStatus string
		var wsName string
		if err := tx.QueryRow(r.Context(), `
			SELECT wi.status::text, w.name
			FROM workspace_invitations wi
			JOIN workspaces w ON w.id = wi.workspace_id
			WHERE wi.id = $1 AND wi.workspace_id = $2
			FOR UPDATE OF wi
		`, invID, wc.WorkspaceID).Scan(&currentStatus, &wsName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				notFound = true
				return nil
			}
			return err
		}
		if currentStatus != "PENDING" && currentStatus != "EXPIRED" {
			notPending = true
			return nil
		}

		return tx.QueryRow(r.Context(), `
			UPDATE workspace_invitations
			SET token_hash = $3,
			    encrypted_pending_token = $4,
			    expires_at = $5,
			    status = 'PENDING',
			    workspace_name = $6
			WHERE id = $1 AND workspace_id = $2
			RETURNING id, workspace_id, email, role, status::text, expires_at, created_at, invited_by_email
		`, invID, wc.WorkspaceID, tokenHash, sealedToken, newExpiry, wsName).Scan(
			&dto.ID, &dto.WorkspaceID, &dto.Email, &dto.Role,
			&dto.Status, &dto.ExpiresAt, &dto.CreatedAt, &dto.InvitedByEmail,
		)
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_resend_invitation")
		return
	}
	if notFound {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	if notPending {
		writeErr(w, http.StatusConflict, "invitation_not_pending")
		return
	}

	if _, err := h.inviteWorkflow.Start(r.Context(), temporalinternal.SyncScope{
		WorkspaceID: wc.WorkspaceID,
		UserID:      wc.UserID,
	}, dto.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_dispatch_invitation")
		return
	}

	writeJSON(w, http.StatusOK, dto)
}

// RevokeInvitation marks a pending invitation REVOKED and clears any staged token.
func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	invID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_invitation_id")
		return
	}

	var notFound bool
	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		tag, execErr := tx.Exec(r.Context(), `
			UPDATE workspace_invitations
			SET status = 'REVOKED', encrypted_pending_token = NULL
			WHERE id = $1 AND workspace_id = $2 AND status IN ('PENDING', 'EXPIRED')
		`, invID, wc.WorkspaceID)
		if execErr != nil {
			return execErr
		}
		if tag.RowsAffected() == 0 {
			notFound = true
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_revoke_invitation")
		return
	}
	if notFound {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMembers returns all members of the active workspace from workspace_roster.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	members := make([]MemberDTO, 0)
	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		rows, qErr := tx.Query(r.Context(), `
			SELECT user_id, email, display_name, role, created_at
			FROM workspace_roster
			WHERE workspace_id = $1
			ORDER BY (role = 'owner') DESC, created_at ASC
		`, wc.WorkspaceID)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()

		for rows.Next() {
			var m MemberDTO
			if scanErr := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.CreatedAt); scanErr != nil {
				return scanErr
			}
			m.IsCurrentUser = m.UserID == wc.UserID
			members = append(members, m)
		}
		return rows.Err()
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_list_members")
		return
	}

	writeJSON(w, http.StatusOK, members)
}

// UpdateMemberRole allows the workspace owner to switch a non-owner member between
// member and read_only.
func (h *Handler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	targetUserID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_user_id")
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	role := strings.TrimSpace(req.Role)
	if role != string(middleware.RoleMember) && role != string(middleware.RoleReadOnly) {
		writeErr(w, http.StatusBadRequest, "invalid_role")
		return
	}

	var updated MemberDTO
	var notFound, cannotModifyOwner bool

	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		var existingRole string
		if err := tx.QueryRow(r.Context(), `
			SELECT role FROM workspace_members
			WHERE workspace_id = $1 AND user_id = $2
		`, wc.WorkspaceID, targetUserID).Scan(&existingRole); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				notFound = true
				return nil
			}
			return err
		}
		if existingRole == string(middleware.RoleOwner) || targetUserID == wc.UserID {
			cannotModifyOwner = true
			return nil
		}

		if _, err := tx.Exec(r.Context(), `
			UPDATE workspace_members SET role = $3
			WHERE workspace_id = $1 AND user_id = $2
		`, wc.WorkspaceID, targetUserID, role); err != nil {
			return err
		}

		err := tx.QueryRow(r.Context(), `
			UPDATE workspace_roster SET role = $3
			WHERE workspace_id = $1 AND user_id = $2
			RETURNING user_id, email, display_name, role, created_at
		`, wc.WorkspaceID, targetUserID, role).Scan(
			&updated.UserID, &updated.Email, &updated.DisplayName, &updated.Role, &updated.CreatedAt,
		)
		return err
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_member_role")
		return
	}
	if notFound {
		writeErr(w, http.StatusNotFound, "member_not_found")
		return
	}
	if cannotModifyOwner {
		writeErr(w, http.StatusBadRequest, "cannot_modify_owner_role")
		return
	}

	updated.IsCurrentUser = updated.UserID == wc.UserID
	writeJSON(w, http.StatusOK, updated)
}

// RemoveMember removes a member from the workspace (owner-only) or allows a non-owner
// member to leave the shared workspace themselves. Owners cannot remove themselves.
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	wc, err := middleware.GetWorkspaceContext(r.Context())
	if err != nil {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	targetUserID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_user_id")
		return
	}

	if targetUserID == wc.UserID {
		if wc.Role == middleware.RoleOwner {
			writeErr(w, http.StatusBadRequest, "cannot_remove_sole_owner")
			return
		}
	} else if wc.Role != middleware.RoleOwner {
		writeErr(w, http.StatusForbidden, "forbidden")
		return
	}

	var notFound, targetIsOwner bool
	err = db.WithWorkspaceTx(r.Context(), h.pool, wc.UserID, func(tx pgx.Tx) error {
		var targetRole string
		if err := tx.QueryRow(r.Context(), `
			SELECT role FROM workspace_members
			WHERE workspace_id = $1 AND user_id = $2
		`, wc.WorkspaceID, targetUserID).Scan(&targetRole); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				notFound = true
				return nil
			}
			return err
		}
		if targetRole == string(middleware.RoleOwner) {
			targetIsOwner = true
			return nil
		}

		// Delete from workspace_roster BEFORE workspace_members so self-leave still passes
		// workspace_roster's RLS WITH CHECK/USING subquery on workspaces.
		if _, err := tx.Exec(r.Context(), `
			DELETE FROM workspace_roster WHERE workspace_id = $1 AND user_id = $2
		`, wc.WorkspaceID, targetUserID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2
		`, wc.WorkspaceID, targetUserID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_remove_member")
		return
	}
	if notFound {
		writeErr(w, http.StatusNotFound, "member_not_found")
		return
	}
	if targetIsOwner {
		writeErr(w, http.StatusBadRequest, "cannot_remove_sole_owner")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) optionalJWTClaims(r *http.Request) (middleware.TokenClaims, bool, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" || !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return middleware.TokenClaims{}, false, nil
	}
	rawToken := strings.TrimSpace(authHeader[7:])
	if rawToken == "" {
		return middleware.TokenClaims{}, false, nil
	}
	if h.verifier == nil {
		return middleware.TokenClaims{}, true, middleware.ErrInvalidToken
	}
	if cv, ok := h.verifier.(middleware.ClaimsVerifier); ok {
		claims, err := cv.VerifyClaims(r.Context(), rawToken)
		if err != nil {
			return middleware.TokenClaims{}, true, err
		}
		return claims, true, nil
	}
	sub, err := h.verifier.Verify(r.Context(), rawToken)
	if err != nil {
		return middleware.TokenClaims{}, true, err
	}
	return middleware.TokenClaims{Subject: sub}, true, nil
}

// PreviewInvitation returns non-sensitive invitation metadata for /invite?token=...
func (h *Handler) PreviewInvitation(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(r.URL.Query().Get("token"))
	if rawToken == "" {
		writeErr(w, http.StatusBadRequest, "token_required")
		return
	}
	tokenHash := hashInviteToken(rawToken)

	tx, err := db.BeginInviteTokenTx(r.Context(), h.pool, tokenHash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_invitation")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var dto InvitePreviewDTO
	err = tx.QueryRow(r.Context(), `
		SELECT workspace_name, email, invited_by_email, role, status::text, expires_at
		FROM workspace_invitations
		WHERE token_hash = $1
	`, tokenHash).Scan(
		&dto.WorkspaceName, &dto.Email, &dto.InvitedByEmail, &dto.Role, &dto.Status, &dto.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_load_invitation")
		return
	}

	dto.Status = effectiveInviteStatus(dto.Status, dto.ExpiresAt, time.Now().UTC())

	if claims, hasToken, cErr := h.optionalJWTClaims(r); hasToken && cErr == nil && claims.Email != "" {
		dto.SignedInEmail = claims.Email
		dto.EmailMismatch = !strings.EqualFold(claims.Email, dto.Email)
	}

	writeJSON(w, http.StatusOK, dto)
}

// AcceptInvitation validates the raw invite token, enforces strict email matching when
// authenticated, provisions/links the Authentik (or local Dex) user, adds them to
// workspace_members + workspace_roster, and marks the invitation ACCEPTED.
func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	rawToken := strings.TrimSpace(req.Token)
	if rawToken == "" {
		writeErr(w, http.StatusBadRequest, "token_required")
		return
	}

	claims, hasBearer, cErr := h.optionalJWTClaims(r)
	if hasBearer && cErr != nil {
		writeErr(w, http.StatusUnauthorized, "invalid_token")
		return
	}

	tokenHash := hashInviteToken(rawToken)

	// Phase 1: Validate invitation state and strict email match under invite_token_hash RLS.
	tx, err := db.BeginInviteTokenTx(r.Context(), h.pool, tokenHash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_validate_invitation")
		return
	}

	var (
		invID         uuid.UUID
		workspaceID   uuid.UUID
		workspaceName string
		inviteEmail   string
		inviteRole    string
		status        string
		expiresAt     time.Time
	)
	err = tx.QueryRow(r.Context(), `
		SELECT id, workspace_id, workspace_name, email, role, status::text, expires_at
		FROM workspace_invitations
		WHERE token_hash = $1
	`, tokenHash).Scan(&invID, &workspaceID, &workspaceName, &inviteEmail, &inviteRole, &status, &expiresAt)
	_ = tx.Rollback(r.Context())

	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_validate_invitation")
		return
	}

	if status == "ACCEPTED" || status == "REVOKED" {
		writeErr(w, http.StatusConflict, "invitation_already_"+strings.ToLower(status))
		return
	}
	if status == "EXPIRED" || !expiresAt.After(time.Now().UTC()) {
		writeErr(w, http.StatusGone, "invitation_expired")
		return
	}

	if hasBearer && claims.Email != "" && !strings.EqualFold(claims.Email, inviteEmail) {
		writeErr(w, http.StatusForbidden, "email_mismatch")
		return
	}

	displayName := strings.TrimSpace(req.Name)
	if displayName == "" && hasBearer {
		displayName = claims.Name
	}

	// Phase 2: Provision or look up user in Identity Provider (Authentik v3 or local Dex noop).
	provisioned, err := h.idpProvisioner.EnsureUser(r.Context(), inviteEmail, displayName)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "failed_to_provision_identity")
		return
	}

	// If the caller presented a valid JWT and no IdP API token is configured (or UUIDs
	// differ in local dev), trust the verified JWT subject when emails already matched.
	if hasBearer && claims.Email != "" {
		if jwtUID, parseErr := uuid.Parse(claims.Subject); parseErr == nil && jwtUID != uuid.Nil {
			provisioned.UserUUID = jwtUID
		}
	}

	// Phase 3: Atomically insert membership + roster and mark invitation ACCEPTED.
	acceptTx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_accept_invitation")
		return
	}
	defer func() { _ = acceptTx.Rollback(r.Context()) }()

	if _, err := acceptTx.Exec(r.Context(), `
		SELECT set_config('app.invite_token_hash', $1, true),
		       set_config('app.current_user_id', $2, true),
		       set_config('app.current_workspace_id', $3, true)
	`, tokenHash, provisioned.UserUUID.String(), workspaceID.String()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_scope_accept_tx")
		return
	}

	var lockedStatus string
	var lockedExpires time.Time
	if err := acceptTx.QueryRow(r.Context(), `
		SELECT status::text, expires_at
		FROM workspace_invitations
		WHERE id = $1
		FOR UPDATE
	`, invID).Scan(&lockedStatus, &lockedExpires); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_lock_invitation")
		return
	}
	if lockedStatus == "ACCEPTED" || lockedStatus == "REVOKED" {
		writeErr(w, http.StatusConflict, "invitation_already_"+strings.ToLower(lockedStatus))
		return
	}
	if lockedStatus == "EXPIRED" || !lockedExpires.After(time.Now().UTC()) {
		writeErr(w, http.StatusGone, "invitation_expired")
		return
	}

	if _, err := acceptTx.Exec(r.Context(), `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, workspaceID, provisioned.UserUUID, inviteRole); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_add_workspace_member")
		return
	}

	if _, err := acceptTx.Exec(r.Context(), `
		INSERT INTO workspace_roster (workspace_id, user_id, role, email, display_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, user_id) DO UPDATE
		SET email = EXCLUDED.email,
		    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), workspace_roster.display_name)
	`, workspaceID, provisioned.UserUUID, inviteRole, inviteEmail, provisioned.DisplayName); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_add_workspace_roster")
		return
	}

	if _, err := acceptTx.Exec(r.Context(), `
		UPDATE workspace_invitations
		SET status = 'ACCEPTED',
		    accepted_by_user_id = $2,
		    encrypted_pending_token = NULL
		WHERE id = $1
	`, invID, provisioned.UserUUID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_update_invitation")
		return
	}

	if err := acceptTx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_commit_acceptance")
		return
	}

	redirectURL := h.appPublicURL + "/login"
	if provisioned.IsNewUser && provisioned.PasswordSetupURL != "" {
		redirectURL = provisioned.PasswordSetupURL
	}

	writeJSON(w, http.StatusOK, AcceptInviteResponse{
		WorkspaceID:   workspaceID,
		WorkspaceName: workspaceName,
		Role:          inviteRole,
		IsNewUser:     provisioned.IsNewUser,
		RedirectURL:   redirectURL,
	})
}
