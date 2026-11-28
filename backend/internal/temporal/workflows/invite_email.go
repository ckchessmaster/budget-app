package workflows

import (
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// SendWorkspaceInviteWorkflow dispatches a household invitation email via Resend with
// bounded exponential backoff retries. It carries only SyncScope + InvitationID so raw
// tokens never enter Temporal workflow history.
func SendWorkspaceInviteWorkflow(ctx workflow.Context, input activities.InviteEmailInput) (*activities.InviteEmailResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var inviteActivities *activities.InviteActivities
	var result activities.InviteEmailResult
	if err := workflow.ExecuteActivity(ctx, inviteActivities.SendInviteEmailActivity, input).Get(ctx, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
