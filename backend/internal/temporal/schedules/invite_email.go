package schedules

import (
	"context"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
)

// InviteEmailStarter starts SendWorkspaceInviteWorkflow on Temporal without importing the
// workflow package from API handler packages.
type InviteEmailStarter struct {
	Client client.Client
}

func (s InviteEmailStarter) Start(ctx context.Context, scope temporalinternal.SyncScope, invitationID uuid.UUID) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("temporal client is not configured")
	}
	run, err := s.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("workspace-invite-%s-%s-%d", scope.WorkspaceID.String(), invitationID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.SendWorkspaceInviteWorkflow, activities.InviteEmailInput{
		SyncScope:    scope,
		InvitationID: invitationID,
	})
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
