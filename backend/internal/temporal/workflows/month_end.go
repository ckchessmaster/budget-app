package workflows

import (
	"fmt"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/budget"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// MonthEndRolloverInput configures a month-end rollover workflow execution.
// When SourceMonth is zero, the workflow deterministically rolls over the previous
// calendar month relative to workflow.Now(ctx). When WorkspaceID is nil, all workspaces
// are rolled over.
type MonthEndRolloverInput struct {
	SourceMonth time.Time  `json:"source_month,omitempty"`
	WorkspaceID *uuid.UUID `json:"workspace_id,omitempty"`
}

// MonthEndRolloverResult aggregates rollover outcomes across all processed workspaces.
type MonthEndRolloverResult struct {
	SourceMonth         time.Time                            `json:"source_month"`
	TargetMonth         time.Time                            `json:"target_month"`
	WorkspacesProcessed int                                  `json:"workspaces_processed"`
	WorkspacesSkipped   int                                  `json:"workspaces_skipped"`
	WorkspacesFailed    int                                  `json:"workspaces_failed"`
	Results             []activities.WorkspaceRolloverResult `json:"results"`
	Errors              []string                             `json:"errors,omitempty"`
}

// MonthEndRolloverWorkflow runs at month boundary (0 0 1 * * UTC) to evaluate category
// rollovers, freeze M+1 monthly_category_budgets baselines, record sweep audits, and
// persist cash/credit-card deficit interlocks per workspace.
func MonthEndRolloverWorkflow(ctx workflow.Context, input MonthEndRolloverInput) (*MonthEndRolloverResult, error) {
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
	targetMonth := sourceMonth.AddDate(0, 1, 0)

	var rolloverActivities *activities.RolloverActivities

	var workspaceIDs []uuid.UUID
	if input.WorkspaceID != nil && *input.WorkspaceID != uuid.Nil {
		workspaceIDs = []uuid.UUID{*input.WorkspaceID}
	} else {
		if err := workflow.ExecuteActivity(ctx, rolloverActivities.ListWorkspacesForRolloverActivity).Get(ctx, &workspaceIDs); err != nil {
			return nil, err
		}
	}

	result := &MonthEndRolloverResult{
		SourceMonth: sourceMonth,
		TargetMonth: targetMonth,
		Results:     make([]activities.WorkspaceRolloverResult, 0, len(workspaceIDs)),
	}

	for _, wsID := range workspaceIDs {
		var wsRes activities.WorkspaceRolloverResult
		err := workflow.ExecuteActivity(ctx, rolloverActivities.ExecuteWorkspaceRolloverActivity, activities.ExecuteWorkspaceRolloverInput{
			WorkspaceID: wsID,
			SourceMonth: sourceMonth,
		}).Get(ctx, &wsRes)
		if err != nil {
			result.WorkspacesFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("workspace %s: %v", wsID, err))
			continue
		}

		result.Results = append(result.Results, wsRes)
		if wsRes.Skipped || wsRes.AlreadyExecuted {
			result.WorkspacesSkipped++
		} else {
			result.WorkspacesProcessed++
		}
	}

	if result.WorkspacesFailed > 0 {
		return nil, fmt.Errorf("month-end rollover failed for %d workspace(s): %s", result.WorkspacesFailed, strings.Join(result.Errors, "; "))
	}

	return result, nil
}
