package workflows

import (
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// SimpleFINClaimInput is a scope and nothing else. The setup token is staged in the
// database by the API before this workflow starts, because Temporal records workflow and
// activity inputs in history for the whole retention window.
type SimpleFINClaimInput struct {
	temporalinternal.SyncScope
}

// SimpleFINClaimResult is what the browser polls for. The accounts are the ones the bank
// reported; the user still has to map them, which is why this counts them as discovered
// rather than linked.
type SimpleFINClaimResult struct {
	ConnectionID       string                         `json:"connection_id"`
	Accounts           []activities.DiscoveredAccount `json:"accounts"`
	AccountsDiscovered int                            `json:"accounts_discovered"`
	Warnings           []string                       `json:"warnings,omitempty"`
}

// SimpleFINClaimWorkflow replaces the synchronous claim that used to run inside
// POST /api/v1/bank/simplefin/claim.
//
// The two steps are separate activities on purpose. Claiming is single-use at the bank and
// must never be repeated once it has succeeded, while fetching the account list is the part
// most likely to fail on a flaky bridge. One activity doing both would re-claim on every
// fetch retry.
func SimpleFINClaimWorkflow(ctx workflow.Context, input SimpleFINClaimInput) (*SimpleFINClaimResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 90 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    6,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var claimActivities *activities.ClaimActivities

	var claim activities.ClaimConnectionResult
	if err := workflow.ExecuteActivity(ctx, claimActivities.ClaimConnectionActivity, input.SyncScope).Get(ctx, &claim); err != nil {
		return nil, err
	}

	var discovered *activities.FetchDiscoveredAccountsResult
	if err := workflow.ExecuteActivity(ctx, claimActivities.FetchDiscoveredAccountsActivity, input.SyncScope).Get(ctx, &discovered); err != nil {
		return nil, err
	}

	result := &SimpleFINClaimResult{ConnectionID: claim.ConnectionID}
	if discovered != nil {
		result.Accounts = discovered.Accounts
		result.AccountsDiscovered = len(discovered.Accounts)
		result.Warnings = discovered.Warnings
	}
	return result, nil
}

// SimpleFINDiscoverWorkflow fetches accounts from an already-connected SimpleFIN Bridge
// without claiming a new setup token.
func SimpleFINDiscoverWorkflow(ctx workflow.Context, input SimpleFINClaimInput) (*SimpleFINClaimResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 90 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    6,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var claimActivities *activities.ClaimActivities

	var discovered *activities.FetchDiscoveredAccountsResult
	if err := workflow.ExecuteActivity(ctx, claimActivities.FetchDiscoveredAccountsActivity, input.SyncScope).Get(ctx, &discovered); err != nil {
		return nil, err
	}

	result := &SimpleFINClaimResult{}
	if discovered != nil {
		result.Accounts = discovered.Accounts
		result.AccountsDiscovered = len(discovered.Accounts)
		result.Warnings = discovered.Warnings
	}
	return result, nil
}
