package schedules

import (
	"context"
	"errors"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

const (
	// MonthEndRolloverScheduleID is the Temporal schedule identifier for month-end rollovers.
	MonthEndRolloverScheduleID = "month-end-rollover"
	// MonthEndRolloverCron triggers at 00:00 UTC on the 1st of every month.
	MonthEndRolloverCron = "0 0 1 * *"
)

// BuildMonthEndRolloverScheduleOptions constructs the Temporal ScheduleOptions for MonthEndRolloverWorkflow.
func BuildMonthEndRolloverScheduleOptions() client.ScheduleOptions {
	return client.ScheduleOptions{
		ID: MonthEndRolloverScheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{MonthEndRolloverCron},
			TimeZoneName:    "UTC",
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        "month-end-rollover-wf",
			Workflow:  workflows.MonthEndRolloverWorkflow,
			TaskQueue: temporalinternal.TaskQueue,
			Args:      []any{workflows.MonthEndRolloverInput{}},
		},
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	}
}

// EnsureMonthEndRolloverSchedule idempotently registers the global 0 0 1 * * UTC
// MonthEndRolloverWorkflow schedule in Temporal.
func EnsureMonthEndRolloverSchedule(ctx context.Context, c client.Client) error {
	_, err := c.ScheduleClient().Create(ctx, BuildMonthEndRolloverScheduleOptions())
	if err != nil {
		var alreadyExists *serviceerror.AlreadyExists
		if errors.As(err, &alreadyExists) {
			return nil
		}
		return err
	}
	return nil
}

// TriggerMonthEndRolloverNow starts an immediate on-demand MonthEndRolloverWorkflow execution.
func TriggerMonthEndRolloverNow(ctx context.Context, c client.Client, input workflows.MonthEndRolloverInput) (string, error) {
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        fmt.Sprintf("month-end-rollover-manual-%d", time.Now().UTC().UnixNano()),
		TaskQueue: temporalinternal.TaskQueue,
	}, workflows.MonthEndRolloverWorkflow, input)
	if err != nil {
		return "", err
	}
	return run.GetID(), nil
}
