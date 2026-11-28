package workflows

import (
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// SendPlatformInviteWorkflow dispatches an optional platform signup invitation email
// via Resend with bounded exponential backoff retries.
func SendPlatformInviteWorkflow(ctx workflow.Context, input activities.PlatformInviteEmailInput) (*activities.PlatformInviteEmailResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var platformInviteActivities *activities.PlatformInviteActivities
	var result activities.PlatformInviteEmailResult
	if err := workflow.ExecuteActivity(ctx, platformInviteActivities.SendPlatformInviteEmailActivity, input).Get(ctx, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
