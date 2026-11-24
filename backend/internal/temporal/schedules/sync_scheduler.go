package schedules

import (
	"context"
	"errors"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// EnsureWorkspaceSyncSchedule registers a 6-hour recurring schedule (with 15m jitter)
// for BankSyncWorkflow on the workspace.
func EnsureWorkspaceSyncSchedule(ctx context.Context, c client.Client, scope temporalinternal.SyncScope) error {
	workspaceID := scope.WorkspaceID
	scheduleID := fmt.Sprintf("bank-sync-%s", workspaceID.String())

	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: scheduleID,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{
				{Every: 6 * time.Hour},
			},
			Jitter: 15 * time.Minute,
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        fmt.Sprintf("bank-sync-wf-%s", workspaceID.String()),
			Workflow:  workflows.BankSyncWorkflow,
			TaskQueue: temporalinternal.TaskQueue,
			Args:      []any{workflows.BankSyncInput{SyncScope: scope}},
		},
	})
	if err != nil {
		var alreadyExists *serviceerror.AlreadyExists
		if errors.As(err, &alreadyExists) {
			return nil
		}
		return err
	}
	return nil
}

// TriggerSyncNow starts an immediate on-demand BankSyncWorkflow execution.
func TriggerSyncNow(ctx context.Context, c client.Client, scope temporalinternal.SyncScope) (string, error) {
	workspaceID := scope.WorkspaceID
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("bank-sync-manual-%s-%d", workspaceID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.BankSyncWorkflow, workflows.BankSyncInput{SyncScope: scope})
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
