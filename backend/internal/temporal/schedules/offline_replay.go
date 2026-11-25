package schedules

import (
	"context"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"go.temporal.io/sdk/client"
)

// OfflineReplayStarter starts a durable replay of a queued offline batch.
//
// It satisfies transaction.ReplayWorkflow. The transaction package cannot import the
// workflow packages itself — the replay activities import its service — so the dependency
// is inverted through that interface and wired up in the server, exactly as the SimpleFIN
// claim and the demo reset do.
type OfflineReplayStarter struct {
	Client client.Client
}

// Start submits the batch and returns the workflow id the client polls for.
//
// The id carries a timestamp, unlike demo-reset-<workspace>, which deliberately does not.
// Two queued batches are genuinely different work — the client may add items and flush
// again while an earlier batch is still draining — and per-item idempotency is what keeps
// them from double-applying, not a unique workflow id. The workspace id still has to appear
// in the id: the shared status endpoint authorizes a read by checking for it.
func (s OfflineReplayStarter) Start(
	ctx context.Context,
	scope temporalinternal.SyncScope,
	items []temporalinternal.ReplayItem,
) (string, error) {
	if s.Client == nil {
		return "", fmt.Errorf("temporal client is not configured")
	}
	if len(items) == 0 {
		return "", fmt.Errorf("an offline replay needs at least one item")
	}
	if len(items) > temporalinternal.MaxReplayBatchItems {
		return "", fmt.Errorf("an offline replay batch may carry at most %d items, got %d",
			temporalinternal.MaxReplayBatchItems, len(items))
	}

	run, err := s.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("offline-replay-%s-%d", scope.WorkspaceID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
		// A batch of a hundred items with retries has a bounded worst case, and a client
		// should not have to guess past it: without a run timeout an item stuck behind an
		// unhealthy worker leaves the queue in limbo indefinitely.
		WorkflowRunTimeout: 30 * time.Minute,
	}, workflows.OfflineReplayWorkflow, workflows.OfflineReplayInput{
		SyncScope: scope,
		Items:     items,
	})
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
