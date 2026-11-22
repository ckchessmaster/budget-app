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

type BankSyncResult struct {
	FetchedCount       int      `json:"fetched_count"`
	InsertedCount      int      `json:"inserted_count"`
	SettledCount       int      `json:"settled_count"`
	ManualMatchedCount int      `json:"manual_matched_count"`
	TransfersLinked    int      `json:"transfers_linked"`
	NormalizedCount    int      `json:"normalized_count"`
	Warnings           []string `json:"warnings,omitempty"`
}

// BankSyncWorkflow orchestrates fetching SimpleFIN data, persisting/reconciling transactions,
// detecting cross-account transfers, and normalizing merchant names.
func BankSyncWorkflow(ctx workflow.Context, input BankSyncInput) (*BankSyncResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    3 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var bankActivities *activities.BankActivities

	var data *simplefin.DataResponse
	if err := workflow.ExecuteActivity(ctx, bankActivities.FetchSimpleFINDataActivity, input.WorkspaceID).Get(ctx, &data); err != nil {
		return nil, err
	}

	var persistRes activities.PersistTransactionsResult
	if err := workflow.ExecuteActivity(ctx, bankActivities.PersistTransactionsActivity, activities.PersistTransactionsInput{
		WorkspaceID: input.WorkspaceID,
		Data:        data,
	}).Get(ctx, &persistRes); err != nil {
		return nil, err
	}

	var linked int
	if err := workflow.ExecuteActivity(ctx, bankActivities.DetectAndLinkTransfersActivity, input.WorkspaceID).Get(ctx, &linked); err != nil {
		return nil, err
	}

	var normalized int
	if err := workflow.ExecuteActivity(ctx, bankActivities.NormalizeMerchantsActivity, input.WorkspaceID).Get(ctx, &normalized); err != nil {
		return nil, err
	}

	return &BankSyncResult{
		FetchedCount:       persistRes.FetchedCount,
		InsertedCount:      persistRes.InsertedCount,
		SettledCount:       persistRes.SettledCount,
		ManualMatchedCount: persistRes.ManualMatchedCount,
		TransfersLinked:    linked,
		NormalizedCount:    normalized,
		Warnings:           persistRes.Warnings,
	}, nil
}
