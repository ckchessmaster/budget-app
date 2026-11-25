package activities

import (
	"context"

	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/google/uuid"
)

// EvaluateNotificationsActivity runs spending velocity and statement due-date checks
// after a workspace bank sync completes.
func (a *BankActivities) EvaluateNotificationsActivity(ctx context.Context, scope temporalinternal.SyncScope) (notifications.EvaluationResult, error) {
	if a.NotificationEvaluator == nil || scope.WorkspaceID == uuid.Nil || scope.UserID == uuid.Nil {
		return notifications.EvaluationResult{}, nil
	}
	return a.NotificationEvaluator.EvaluateWorkspace(ctx, scope.WorkspaceID, scope.UserID)
}
