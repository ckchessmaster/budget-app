package activities

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/email"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

// PlatformInviteActivities dispatches optional platform invitation emails without
// placing raw invite tokens into Temporal history.
type PlatformInviteActivities struct {
	Pool         *pgxpool.Pool
	Vault        crypto.Vault
	Sender       email.Sender
	AppPublicURL string
}

type PlatformInviteEmailInput struct {
	InvitationID uuid.UUID `json:"invitation_id"`
}

type PlatformInviteEmailResult struct {
	InvitationID string `json:"invitation_id"`
	MessageID    string `json:"message_id,omitempty"`
	Skipped      bool   `json:"skipped,omitempty"`
}

func (a *PlatformInviteActivities) SendPlatformInviteEmailActivity(ctx context.Context, input PlatformInviteEmailInput) (PlatformInviteEmailResult, error) {
	if input.InvitationID == uuid.Nil {
		return PlatformInviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"invitation_id is required", "InvalidPlatformInviteInput", nil)
	}
	if a.Pool == nil || a.Vault == nil || a.Sender == nil {
		return PlatformInviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"platform invite activity dependencies are not configured", "PlatformInviteActivityUnconfigured", nil)
	}

	var (
		label          string
		recipientEmail string
		inviterEmail   string
		tokenHash      string
		sealedToken    string
		status         string
		expiresAt      time.Time
	)

	err := db.WithPlatformAdminTx(ctx, a.Pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT pi.label, pi.recipient_email, COALESCE(pu.email, ''), pi.token_hash,
			       COALESCE(pi.encrypted_pending_token, ''), pi.status::text, pi.expires_at
			FROM platform_invitations pi
			LEFT JOIN platform_users pu ON pu.user_id = pi.created_by_user_id
			WHERE pi.id = $1
		`, input.InvitationID).Scan(
			&label, &recipientEmail, &inviterEmail, &tokenHash,
			&sealedToken, &status, &expiresAt,
		)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformInviteEmailResult{}, temporal.NewNonRetryableApplicationError(
				"platform invitation not found", "PlatformInviteNotFound", nil)
		}
		return PlatformInviteEmailResult{}, err
	}

	if status != "ACTIVE" || sealedToken == "" || strings.TrimSpace(recipientEmail) == "" {
		return PlatformInviteEmailResult{InvitationID: input.InvitationID.String(), Skipped: true}, nil
	}
	if !expiresAt.After(time.Now().UTC()) {
		return PlatformInviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"platform invitation has already expired", "PlatformInviteExpired", nil)
	}

	rawToken, err := a.Vault.Decrypt(sealedToken)
	if err != nil {
		return PlatformInviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"failed to decrypt pending platform invitation token", "PlatformInviteDecryptError", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(a.AppPublicURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:5173"
	}
	inviteURL := baseURL + "/signup?invite=" + url.QueryEscape(rawToken)

	subject, htmlBody, textBody := email.RenderPlatformInviteEmail(label, inviterEmail, inviteURL, expiresAt)
	hashPrefix := tokenHash
	if len(hashPrefix) > 16 {
		hashPrefix = hashPrefix[:16]
	}
	idemKey := fmt.Sprintf("platform-invite-%s-%s", input.InvitationID.String(), hashPrefix)

	msgID, sendErr := a.Sender.Send(ctx, email.OutboundEmail{
		To:             strings.TrimSpace(recipientEmail),
		Subject:        subject,
		HTMLBody:       htmlBody,
		TextBody:       textBody,
		IdempotencyKey: idemKey,
		Template:       "platform_invite",
	})
	if sendErr != nil {
		var apiErr *email.APIError
		if errors.As(sendErr, &apiErr) {
			if apiErr.IsTerminal() {
				return PlatformInviteEmailResult{}, temporal.NewNonRetryableApplicationError(
					apiErr.Error(), "PlatformInviteEmailRejected", sendErr)
			}
			if apiErr.RetryAfter > 0 {
				return PlatformInviteEmailResult{}, temporal.NewApplicationErrorWithOptions(
					apiErr.Error(), "PlatformInviteEmailRateLimited",
					temporal.ApplicationErrorOptions{Cause: sendErr, NextRetryDelay: apiErr.RetryAfter})
			}
		}
		return PlatformInviteEmailResult{}, sendErr
	}

	if err := db.WithPlatformAdminTx(ctx, a.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE platform_invitations
			SET encrypted_pending_token = NULL
			WHERE id = $1 AND token_hash = $2
		`, input.InvitationID, tokenHash)
		return err
	}); err != nil {
		return PlatformInviteEmailResult{}, err
	}

	return PlatformInviteEmailResult{
		InvitationID: input.InvitationID.String(),
		MessageID:    msgID,
	}, nil
}
