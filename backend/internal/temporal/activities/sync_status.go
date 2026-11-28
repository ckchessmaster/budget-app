package activities

import (
	"context"
	"regexp"
	"strings"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RecordSyncFailureInput struct {
	temporalinternal.SyncScope
	ErrorMessage string `json:"error_message"`
}

var (
	urlWithCredsPattern = regexp.MustCompile(`https?://[^\s"']+`)
	longDigitsPattern   = regexp.MustCompile(`\b\d{6,}\b`)
)

// SanitizeSyncError strips URLs, embedded credentials, and 6+ digit account/card numbers
// before persisting an error summary for platform health telemetry.
func SanitizeSyncError(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "sync failed"
	}
	cleaned = urlWithCredsPattern.ReplaceAllString(cleaned, "[redacted-url]")
	cleaned = longDigitsPattern.ReplaceAllString(cleaned, "[redacted]")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if len(cleaned) > 200 {
		cleaned = cleaned[:197] + "..."
	}
	return cleaned
}

// RecordSyncFailureActivity records a sanitized error status on simplefin_connections
// under workspace RLS when BankSyncWorkflow fails.
func (a *BankActivities) RecordSyncFailureActivity(ctx context.Context, input RecordSyncFailureInput) error {
	if a.Pool == nil || input.WorkspaceID == uuid.Nil || input.UserID == uuid.Nil {
		return nil
	}
	ctx = db.WithWorkspaceID(ctx, input.WorkspaceID)
	sanitized := SanitizeSyncError(input.ErrorMessage)
	return db.WithWorkspaceTx(ctx, a.Pool, input.UserID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE simplefin_connections
			SET last_sync_status = 'ERROR',
			    last_sync_error = $2
			WHERE workspace_id = $1
		`, input.WorkspaceID, sanitized)
		return err
	})
}
