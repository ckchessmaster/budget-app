package workflows

import (
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type BankSyncInput struct {
	temporalinternal.SyncScope
}

type BankSyncResult struct {
	FetchedCount         int      `json:"fetched_count"`
	InsertedCount        int      `json:"inserted_count"`
	SettledCount         int      `json:"settled_count"`
	ManualMatchedCount   int      `json:"manual_matched_count"`
	TransfersLinked      int      `json:"transfers_linked"`
	NormalizedCount      int      `json:"normalized_count"`
	AutoCategorizedCount int      `json:"auto_categorized_count,omitempty"`
	NotificationsSent    int      `json:"notifications_sent,omitempty"`
	Warnings             []string `json:"warnings,omitempty"`
}

// BankSyncWorkflow orchestrates fetching SimpleFIN data, persisting/reconciling transactions,
// detecting cross-account transfers, normalizing merchant names, applying categorization rules,
// and evaluating alert triggers.
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

	recordFailure := func(syncErr error) {
		failCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 15 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    1 * time.Second,
				BackoffCoefficient: 2.0,
				MaximumAttempts:    2,
			},
		})
		if err := workflow.ExecuteActivity(failCtx, bankActivities.RecordSyncFailureActivity, activities.RecordSyncFailureInput{
			SyncScope:    input.SyncScope,
			ErrorMessage: syncErr.Error(),
		}).Get(failCtx, nil); err != nil {
			workflow.GetLogger(ctx).Warn("failed to record SimpleFIN sync failure status", "error", err)
		}
	}

	var data *simplefin.DataResponse
	if err := workflow.ExecuteActivity(ctx, bankActivities.FetchSimpleFINDataActivity, input.SyncScope).Get(ctx, &data); err != nil {
		recordFailure(err)
		return nil, err
	}

	var persistRes activities.PersistTransactionsResult
	if err := workflow.ExecuteActivity(ctx, bankActivities.PersistTransactionsActivity, activities.PersistTransactionsInput{
		SyncScope: input.SyncScope,
		Data:      data,
	}).Get(ctx, &persistRes); err != nil {
		recordFailure(err)
		return nil, err
	}

	var linked int
	if err := workflow.ExecuteActivity(ctx, bankActivities.DetectAndLinkTransfersActivity, input.SyncScope).Get(ctx, &linked); err != nil {
		recordFailure(err)
		return nil, err
	}

	var normalized int
	if err := workflow.ExecuteActivity(ctx, bankActivities.NormalizeMerchantsActivity, input.SyncScope).Get(ctx, &normalized); err != nil {
		recordFailure(err)
		return nil, err
	}

	var autoCategorized int
	if err := workflow.ExecuteActivity(ctx, bankActivities.ApplyCategorizationRulesActivity, input.SyncScope).Get(ctx, &autoCategorized); err != nil {
		recordFailure(err)
		return nil, err
	}

	warnings := append([]string(nil), persistRes.Warnings...)
	notifCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    15 * time.Second,
			MaximumAttempts:    3,
		},
	})
	var notifRes notifications.EvaluationResult
	if err := workflow.ExecuteActivity(notifCtx, bankActivities.EvaluateNotificationsActivity, input.SyncScope).Get(notifCtx, &notifRes); err != nil {
		workflow.GetLogger(ctx).Warn("notification evaluation failed after bank sync", "error", err)
		warnings = append(warnings, "notification evaluation failed")
	}

	return &BankSyncResult{
		FetchedCount:         persistRes.FetchedCount,
		InsertedCount:        persistRes.InsertedCount,
		SettledCount:         persistRes.SettledCount,
		ManualMatchedCount:   persistRes.ManualMatchedCount,
		TransfersLinked:      linked,
		NormalizedCount:      normalized,
		AutoCategorizedCount: autoCategorized,
		NotificationsSent:    notifRes.TotalSent(),
		Warnings:             warnings,
	}, nil
}
