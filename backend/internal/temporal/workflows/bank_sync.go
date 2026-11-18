package workflows

import (
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type BankSyncInput struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

// BankSyncWorkflow orchestrates fetching SimpleFIN data, persisting/reconciling transactions,
// detecting cross-account transfers, and normalizing merchant names.
func BankSyncWorkflow(ctx workflow.Context, input BankSyncInput) error {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    15 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var bankActivities *activities.BankActivities

	var data *simplefin.DataResponse
	if err := workflow.ExecuteActivity(ctx, bankActivities.FetchSimpleFINDataActivity, input.WorkspaceID).Get(ctx, &data); err != nil {
		return err
	}

	var persisted int
	if err := workflow.ExecuteActivity(ctx, bankActivities.PersistTransactionsActivity, activities.PersistTransactionsInput{
		WorkspaceID: input.WorkspaceID,
		Data:        data,
	}).Get(ctx, &persisted); err != nil {
		return err
	}

	var linked int
	if err := workflow.ExecuteActivity(ctx, bankActivities.DetectAndLinkTransfersActivity, input.WorkspaceID).Get(ctx, &linked); err != nil {
		return err
	}

	var normalized int
	if err := workflow.ExecuteActivity(ctx, bankActivities.NormalizeMerchantsActivity, input.WorkspaceID).Get(ctx, &normalized); err != nil {
		return err
	}

	return nil
}
