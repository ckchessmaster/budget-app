package schedules

import (
	"context"
	"errors"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

const (
	// MonthEndRolloverCron triggers at 00:00 UTC on the 1st of every month.
	MonthEndRolloverCron = "0 0 1 * *"
)

// BuildWorkspaceRolloverScheduleOptions constructs the Temporal ScheduleOptions for a
// workspace's MonthEndRolloverWorkflow execution.
func BuildWorkspaceRolloverScheduleOptions(scope temporalinternal.SyncScope) client.ScheduleOptions {
	wsID := scope.WorkspaceID.String()
	return client.ScheduleOptions{
		ID: fmt.Sprintf("month-end-rollover-%s", wsID),
		Spec: client.ScheduleSpec{
			CronExpressions: []string{MonthEndRolloverCron},
			TimeZoneName:    "UTC",
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        fmt.Sprintf("month-end-rollover-wf-%s", wsID),
			Workflow:  workflows.MonthEndRolloverWorkflow,
			TaskQueue: temporalinternal.TaskQueue,
			Args:      []any{workflows.MonthEndRolloverInput{SyncScope: scope}},
		},
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	}
}

// EnsureWorkspaceRolloverSchedule idempotently registers the 0 0 1 * * UTC
// MonthEndRolloverWorkflow schedule for a scoped workspace in Temporal.
func EnsureWorkspaceRolloverSchedule(ctx context.Context, c client.Client, scope temporalinternal.SyncScope) error {
	if scope.WorkspaceID == uuid.Nil || scope.UserID == uuid.Nil {
		return errors.New("workspace_id and user_id are required to schedule month-end rollover")
	}

	_, err := c.ScheduleClient().Create(ctx, BuildWorkspaceRolloverScheduleOptions(scope))
	if err != nil {
		var alreadyExists *serviceerror.AlreadyExists
		if errors.As(err, &alreadyExists) {
			return nil
		}
		return err
	}
	return nil
}

// TriggerMonthEndRolloverNow starts an immediate on-demand MonthEndRolloverWorkflow execution
// for a scoped workspace.
func TriggerMonthEndRolloverNow(ctx context.Context, c client.Client, scope temporalinternal.SyncScope, sourceMonth time.Time) (string, error) {
	if scope.WorkspaceID == uuid.Nil || scope.UserID == uuid.Nil {
		return "", errors.New("workspace_id and user_id are required to trigger month-end rollover")
	}

	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("month-end-rollover-manual-%s-%d", scope.WorkspaceID.String(), time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.MonthEndRolloverWorkflow, workflows.MonthEndRolloverInput{
		SyncScope:   scope,
		SourceMonth: sourceMonth,
	})
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
