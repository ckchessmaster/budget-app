package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/idp"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,64}$`)

// ValidateSignupInvite validates a raw platform invite token via GET /api/v1/signup/validate?invite=...
func (h *Handler) ValidateSignupInvite(w http.ResponseWriter, r *http.Request) {
	rawToken := strings.TrimSpace(r.URL.Query().Get("invite"))
	if rawToken == "" {
		writeErr(w, http.StatusBadRequest, "invite_required")
		return
	}
	tokenHash := hashInviteToken(rawToken)

	tx, err := db.BeginPlatformInviteTokenTx(r.Context(), h.pool, tokenHash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_validate_invite")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var (
		status         string
		expiresAt      time.Time
		recipientEmail string
	)
	err = tx.QueryRow(r.Context(), `
		SELECT status::text, expires_at, recipient_email
		FROM platform_invitations
		WHERE token_hash = $1
	`, tokenHash).Scan(&status, &expiresAt, &recipientEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_validate_invite")
		return
	}

	effective := effectiveInviteStatus(status, expiresAt, time.Now().UTC())
	switch effective {
	case "REDEEMED":
		writeErr(w, http.StatusConflict, "invitation_already_redeemed")
		return
	case "REVOKED":
		writeErr(w, http.StatusConflict, "invitation_revoked")
		return
	case "EXPIRED":
		writeErr(w, http.StatusGone, "invitation_expired")
		return
	}

	resp := map[string]any{
		"valid":      true,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	}
	if strings.TrimSpace(recipientEmail) != "" {
		resp["email"] = recipientEmail
	}
	writeJSON(w, http.StatusOK, resp)
}

// RedeemSignupInvite atomically validates and consumes a single-use platform invite token,
// provisions the friend's account in Authentik (or local Dex noop), inserts their
// platform_users record, and returns the one-time setup URL.
func (h *Handler) RedeemSignupInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Username string `json:"username"`
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
	if !isValidEmail(req.Email) {
		writeErr(w, http.StatusBadRequest, "invalid_email")
		return
	}
	normalizedEmail := strings.ToLower(strings.TrimSpace(req.Email))
	trimmedName := strings.TrimSpace(req.Name)
	if trimmedName == "" || len(trimmedName) > 255 {
		writeErr(w, http.StatusBadRequest, "name_required")
		return
	}
	trimmedUsername := strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(trimmedUsername) {
		writeErr(w, http.StatusBadRequest, "invalid_username")
		return
	}

	tokenHash := hashInviteToken(rawToken)

	tx, err := db.BeginPlatformAdminSerializableTx(r.Context(), h.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_redeem_invite")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var (
		invID     uuid.UUID
		status    string
		expiresAt time.Time
	)
	err = tx.QueryRow(r.Context(), `
		SELECT id, status::text, expires_at
		FROM platform_invitations
		WHERE token_hash = $1
		FOR UPDATE
	`, tokenHash).Scan(&invID, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "invitation_not_found")
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40001" {
			writeErr(w, http.StatusConflict, "invitation_already_redeemed")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_redeem_invite")
		return
	}

	effective := effectiveInviteStatus(status, expiresAt, time.Now().UTC())
	switch effective {
	case "REDEEMED":
		writeErr(w, http.StatusConflict, "invitation_already_redeemed")
		return
	case "REVOKED":
		writeErr(w, http.StatusConflict, "invitation_revoked")
		return
	case "EXPIRED":
		writeErr(w, http.StatusGone, "invitation_expired")
		return
	}

	var emailExists bool
	if err := tx.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM platform_users WHERE LOWER(email) = LOWER($1))
	`, normalizedEmail).Scan(&emailExists); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed_to_check_existing_user")
		return
	}
	if emailExists {
		writeErr(w, http.StatusConflict, "email_already_registered")
		return
	}

	userUUID, setupURL, err := h.idpAdmin.CreateUserAndRecoveryLink(r.Context(), normalizedEmail, trimmedName, trimmedUsername)
	if err != nil {
		switch {
		case errors.Is(err, idp.ErrEmailAlreadyExists):
			writeErr(w, http.StatusConflict, "email_already_registered")
		case errors.Is(err, idp.ErrUsernameAlreadyExists):
			writeErr(w, http.StatusConflict, "username_already_taken")
		default:
			writeErr(w, http.StatusBadGateway, "identity_provider_error")
		}
		return
	}

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO platform_users (user_id, email, display_name, is_active)
		VALUES ($1, $2, $3, TRUE)
	`, userUUID, normalizedEmail, trimmedName); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "email_already_registered")
			return
		}
		if errors.As(err, &pgErr) && pgErr.Code == "40001" {
			writeErr(w, http.StatusConflict, "invitation_already_redeemed")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_register_user")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		UPDATE platform_invitations
		SET status = 'REDEEMED',
		    redeemed_by_user_id = $1,
		    redeemed_at = NOW(),
		    encrypted_pending_token = NULL
		WHERE id = $2
	`, userUUID, invID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40001" {
			writeErr(w, http.StatusConflict, "invitation_already_redeemed")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_consume_invitation")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40001" {
			writeErr(w, http.StatusConflict, "invitation_already_redeemed")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed_to_commit_signup")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"setup_url": setupURL,
	})
}
