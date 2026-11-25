package schedules

import (
	"context"
	"errors"
	"fmt"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// DemoResetStarter starts the durable demo sandbox reset.
//
// It satisfies demo.ResetWorkflow. The demo package cannot import the workflow packages
// directly — it already feeds the schedule helpers, which feed the workflows — so the
// dependency is inverted through that interface and wired up in the server.
type DemoResetStarter struct {
	Client client.Client
}

func (s DemoResetStarter) Start(
	ctx context.Context,
	scope temporalinternal.SyncScope,
	triggerSync bool,
) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("temporal client is not configured")
	}

	// No timestamp in the ID, deliberately. Temporal refuses to start an execution whose
	// ID is already running, which is exactly the "one reset at a time" rule the endpoint
	// needs; once a reset closes, the same ID is available again for the next one.
	run, err := s.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        temporalinternal.DemoResetWorkflowIDPrefix + scope.WorkspaceID.String(),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.DemoResetWorkflow, workflows.DemoResetInput{
		SyncScope:   scope,
		TriggerSync: triggerSync,
	})
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			return "", fmt.Errorf("%w: %s", temporalinternal.ErrWorkflowAlreadyRunning, alreadyStarted.Message)
		}
		return "", err
	}
	return run.GetID(), nil
}
