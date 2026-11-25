package schedules

import (
	"context"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"go.temporal.io/sdk/client"
)

// SimpleFINClaimStarter starts the durable claim workflow.
//
// It satisfies simplefin.ClaimWorkflow. The API package cannot import the workflow
// packages directly — the claim activities import simplefin for its client — so the
// dependency is inverted through that interface and wired up in the server.
type SimpleFINClaimStarter struct {
	Client client.Client
}

func (s SimpleFINClaimStarter) Start(ctx context.Context, scope temporalinternal.SyncScope) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("temporal client is not configured")
	}

	// The workspace ID has to appear in the workflow ID: the shared status endpoint
	// authorizes a read by checking that the caller's workspace is part of the ID.
	run, err := s.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("simplefin-claim-%s-%d", scope.WorkspaceID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.SimpleFINClaimWorkflow, workflows.SimpleFINClaimInput{SyncScope: scope})
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
