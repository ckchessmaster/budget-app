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

// PlatformInviteEmailStarter starts SendPlatformInviteWorkflow on Temporal without
// importing the workflows package from API handler packages.
type PlatformInviteEmailStarter struct {
	Client client.Client
}

func (s PlatformInviteEmailStarter) Start(ctx context.Context, invitationID uuid.UUID) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("temporal client is not configured")
	}
	run, err := s.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("platform-invite-%s-%d", invitationID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.SendPlatformInviteWorkflow, activities.PlatformInviteEmailInput{
		InvitationID: invitationID,
	})
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
