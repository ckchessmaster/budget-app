package workflows

import (
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// MonthEndRolloverInput configures a per-workspace month-end rollover workflow execution.
// When SourceMonth is zero, the workflow deterministically rolls over the previous
// calendar month relative to workflow.Now(ctx).
type MonthEndRolloverInput struct {
	temporalinternal.SyncScope
	SourceMonth time.Time `json:"source_month,omitempty"`
}

// MonthEndRolloverWorkflow runs at month boundary (0 0 1 * * UTC) for a scoped workspace
// to evaluate category rollovers, freeze M+1 monthly_category_budgets baselines, record
// sweep audits, and persist cash/credit-card deficit interlocks.
func MonthEndRolloverWorkflow(ctx workflow.Context, input MonthEndRolloverInput) (*activities.WorkspaceRolloverResult, error) {
	if input.WorkspaceID == uuid.Nil || input.UserID == uuid.Nil {
		return nil, temporal.NewNonRetryableApplicationError("workspace_id and user_id are required", "InvalidRolloverInput", nil)
	}

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

	var sourceMonth time.Time
	if input.SourceMonth.IsZero() {
		sourceMonth = budget.NormalizeYearMonth(workflow.Now(ctx)).AddDate(0, -1, 0)
	} else {
		sourceMonth = budget.NormalizeYearMonth(input.SourceMonth)
	}

	var rolloverActivities *activities.RolloverActivities
	var wsRes activities.WorkspaceRolloverResult
	if err := workflow.ExecuteActivity(ctx, rolloverActivities.ExecuteWorkspaceRolloverActivity, activities.ExecuteWorkspaceRolloverInput{
		SyncScope:   input.SyncScope,
		SourceMonth: sourceMonth,
	}).Get(ctx, &wsRes); err != nil {
		return nil, err
	}

	return &wsRes, nil
}
