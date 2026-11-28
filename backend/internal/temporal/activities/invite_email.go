package activities

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/email"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

// InviteActivities dispatches household invitation emails without putting raw invite
// tokens into Temporal history. The API seals the raw token into
// workspace_invitations.encrypted_pending_token before starting the workflow, and this
// activity reads, sends, and clears it.
type InviteActivities struct {
	Pool         *pgxpool.Pool
	Vault        crypto.Vault
	Sender       email.Sender
	AppPublicURL string
}

type InviteEmailInput struct {
	temporalinternal.SyncScope
	InvitationID uuid.UUID `json:"invitation_id"`
}

type InviteEmailResult struct {
	InvitationID string `json:"invitation_id"`
	MessageID    string `json:"message_id,omitempty"`
	Skipped      bool   `json:"skipped,omitempty"`
}

func (a *InviteActivities) SendInviteEmailActivity(ctx context.Context, input InviteEmailInput) (InviteEmailResult, error) {
	if input.WorkspaceID == uuid.Nil || input.UserID == uuid.Nil || input.InvitationID == uuid.Nil {
		return InviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"workspace_id, user_id, and invitation_id are required", "InvalidInviteScope", nil)
	}
	if a.Pool == nil || a.Vault == nil || a.Sender == nil {
		return InviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"invite activity dependencies are not configured", "InviteActivityUnconfigured", nil)
	}

	ctx = db.WithWorkspaceID(ctx, input.WorkspaceID)

	var (
		workspaceName string
		inviterEmail  string
		recipient     string
		role          string
		tokenHash     string
		sealedToken   string
		status        string
		expiresAt     time.Time
	)

	err := db.WithWorkspaceTx(ctx, a.Pool, input.UserID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT workspace_name, invited_by_email, email, role, token_hash,
			       COALESCE(encrypted_pending_token, ''), status::text, expires_at
			FROM workspace_invitations
			WHERE id = $1 AND workspace_id = $2
		`, input.InvitationID, input.WorkspaceID).Scan(
			&workspaceName, &inviterEmail, &recipient, &role, &tokenHash,
			&sealedToken, &status, &expiresAt,
		)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return InviteEmailResult{}, temporal.NewNonRetryableApplicationError(
				"invitation not found", "InviteNotFound", nil)
		}
		return InviteEmailResult{}, err
	}

	if status != "PENDING" || sealedToken == "" {
		return InviteEmailResult{InvitationID: input.InvitationID.String(), Skipped: true}, nil
	}
	if !expiresAt.After(time.Now().UTC()) {
		return InviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"invitation has already expired", "InviteExpired", nil)
	}

	rawToken, err := a.Vault.Decrypt(sealedToken)
	if err != nil {
		return InviteEmailResult{}, temporal.NewNonRetryableApplicationError(
			"failed to decrypt pending invitation token", "InviteDecryptError", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(a.AppPublicURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:5173"
	}
	inviteURL := baseURL + "/invite?token=" + url.QueryEscape(rawToken)

	subject, htmlBody, textBody := email.RenderInviteEmail(workspaceName, inviterEmail, role, inviteURL)
	hashPrefix := tokenHash
	if len(hashPrefix) > 16 {
		hashPrefix = hashPrefix[:16]
	}
	idemKey := fmt.Sprintf("invite-%s-%s", input.InvitationID.String(), hashPrefix)

	msgID, sendErr := a.Sender.Send(ctx, email.OutboundEmail{
		To:             recipient,
		Subject:        subject,
		HTMLBody:       htmlBody,
		TextBody:       textBody,
		IdempotencyKey: idemKey,
	})
	if sendErr != nil {
		var apiErr *email.APIError
		if errors.As(sendErr, &apiErr) {
			if apiErr.IsTerminal() {
				return InviteEmailResult{}, temporal.NewNonRetryableApplicationError(
					apiErr.Error(), "InviteEmailRejected", sendErr)
			}
			if apiErr.RetryAfter > 0 {
				return InviteEmailResult{}, temporal.NewApplicationErrorWithOptions(
					apiErr.Error(), "InviteEmailRateLimited",
					temporal.ApplicationErrorOptions{Cause: sendErr, NextRetryDelay: apiErr.RetryAfter})
			}
		}
		return InviteEmailResult{}, sendErr
	}

	// Clear the staged ciphertext only for this exact token hash so a concurrent Resend
	// that rotated token_hash does not lose its newly staged token.
	if err := db.WithWorkspaceTx(ctx, a.Pool, input.UserID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE workspace_invitations
			SET encrypted_pending_token = NULL
			WHERE id = $1 AND workspace_id = $2 AND token_hash = $3
		`, input.InvitationID, input.WorkspaceID, tokenHash)
		return err
	}); err != nil {
		return InviteEmailResult{}, err
	}

	return InviteEmailResult{
		InvitationID: input.InvitationID.String(),
		MessageID:    msgID,
	}, nil
}
