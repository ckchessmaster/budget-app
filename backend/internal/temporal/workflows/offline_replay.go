package workflows

import (
	"errors"
	"fmt"
	"sort"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// replayMaxConcurrency bounds how many items of one batch are in flight at a time.
//
// Every item opens its own Postgres transaction, so an uncapped fan-out of a hundred queued
// entries would open a hundred writers against the same account rows and turn contention
// into the very deadlocks the retry policy is here to absorb. The cap costs one batch a
// little latency and keeps the database boring.
const replayMaxConcurrency = 8

// OfflineReplayInput is the scope plus the queued items.
//
// Items are inline rather than staged in the database the way a SimpleFIN claim token is:
// a queued transaction is not a credential, and Temporal history is where the user can see
// what was applied and when, which is the audit trail this flow previously had nowhere.
type OfflineReplayInput struct {
	temporalinternal.SyncScope
	Items []temporalinternal.ReplayItem `json:"items"`
}

// RejectedReplayItem is one item the batch could not apply.
//
// Retryable is the field the client acts on. False means the item is wrong and stays in the
// queue flagged for the user to edit; true means the server gave up on a condition that may
// clear, so the client keeps it queued and sends it again later.
type RejectedReplayItem struct {
	ClientID  string `json:"client_id"`
	Reason    string `json:"reason"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable"`
}

// OfflineReplayResult is what the browser polls for. Applied lists the client ids that are
// now rows; a client drains exactly those from its local queue.
//
// Neither list is in submission order and nothing may depend on one: the items are
// independent and complete as they finish. The lists are sorted by client id so the payload
// a client sees is stable across polls rather than reshuffling with completion order.
type OfflineReplayResult struct {
	Applied  []string             `json:"applied"`
	Rejected []RejectedReplayItem `json:"rejected"`
	Warnings []string             `json:"warnings,omitempty"`
}

type replayOutcome struct {
	clientID string
	err      error
}

// classifyReplayOutcome turns an activity failure into the verdict the client needs.
//
// A non-retryable application error is the activity saying the item itself is wrong, and its
// type is the machine-readable reason. Anything else that reaches here has already burned
// the retry budget on a transient condition, so it is reported as retryable and the client
// keeps the item queued. Either way the workflow completes: a batch must never fail as a
// whole because one entry did.
func classifyReplayOutcome(clientID string, err error) RejectedReplayItem {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		reason := appErr.Type()
		if reason == "" {
			reason = "invalid_item"
		}
		return RejectedReplayItem{ClientID: clientID, Reason: reason, Message: appErr.Message(), Retryable: false}
	}
	return RejectedReplayItem{
		ClientID:  clientID,
		Reason:    "replay_attempts_exhausted",
		Message:   err.Error(),
		Retryable: true,
	}
}

// OfflineReplayWorkflow applies a batch of queued offline transactions.
//
// It replaces the loop that ran inside POST /api/v1/transactions/sync. That loop made the
// browser responsible for holding a partially applied batch, and any item lost to a
// transient error was either retried by hand or silently dropped when the tab closed. Here
// the batch is durable, each item is retried on its own, and the outcome is readable long
// after the request that submitted it.
func OfflineReplayWorkflow(ctx workflow.Context, input OfflineReplayInput) (*OfflineReplayResult, error) {
	result := &OfflineReplayResult{Applied: []string{}, Rejected: []RejectedReplayItem{}}

	if input.WorkspaceID == uuid.Nil || input.UserID == uuid.Nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"workspace_id and user_id are required to replay queued transactions", "invalid_replay_scope", nil)
	}
	if len(input.Items) > temporalinternal.MaxReplayBatchItems {
		// Checked here as well as at the edge, so a batch that was accepted before the
		// limit changed cannot be replayed into an oversized workflow.
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("a replay batch may carry at most %d items", temporalinternal.MaxReplayBatchItems),
			"batch_too_large", nil)
	}
	if len(input.Items) == 0 {
		return result, nil
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    6,
		},
	}
	actCtx := workflow.WithActivityOptions(ctx, ao)

	var replayActivities *activities.ReplayActivities

	outcomes := workflow.NewBufferedChannel(ctx, len(input.Items))
	semaphore := workflow.NewBufferedChannel(ctx, replayMaxConcurrency)
	// NewWaitGroup rather than a zero value: workflow.WaitGroup is an interface bound to the
	// coroutine scheduler, so the zero value is a nil reference.
	wg := workflow.NewWaitGroup(ctx)

	for _, item := range input.Items {
		// Acquired in the main loop rather than inside the goroutine so the cap holds
		// from the first item, not after they have all been spawned.
		semaphore.Send(ctx, true)
		wg.Add(1)
		workflow.Go(ctx, func(gctx workflow.Context) {
			defer wg.Done()

			// The activity's own result confirms the row id; the batch verdict only needs
			// to know whether the item landed, so the payload is not decoded here.
			err := workflow.ExecuteActivity(actCtx, replayActivities.ReplayTransactionItemActivity,
				activities.ReplayItemInput{SyncScope: input.SyncScope, Item: item}).Get(gctx, nil)

			outcomes.Send(gctx, replayOutcome{clientID: item.ClientID, err: err})

			var release bool
			semaphore.Receive(gctx, &release)
		})
	}
	wg.Wait(ctx)
	outcomes.Close()

	for {
		var outcome replayOutcome
		if !outcomes.Receive(ctx, &outcome) {
			break
		}
		if outcome.err != nil {
			result.Rejected = append(result.Rejected, classifyReplayOutcome(outcome.clientID, outcome.err))
			continue
		}
		result.Applied = append(result.Applied, outcome.clientID)
	}

	sort.Strings(result.Applied)
	sort.Slice(result.Rejected, func(i, j int) bool { return result.Rejected[i].ClientID < result.Rejected[j].ClientID })

	if len(result.Applied) > 0 {
		var bankActivities *activities.BankActivities
		notifCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				InitialInterval:    2 * time.Second,
				BackoffCoefficient: 2.0,
				MaximumInterval:    15 * time.Second,
				MaximumAttempts:    3,
			},
		})
		if err := workflow.ExecuteActivity(notifCtx, bankActivities.EvaluateNotificationsActivity, input.SyncScope).Get(notifCtx, nil); err != nil {
			// The transactions are committed. Failing the batch over an alert would
			// tell the client to resend rows that already landed.
			workflow.GetLogger(ctx).Warn("notification evaluation failed after offline replay", "error", err)
			result.Warnings = append(result.Warnings, "notification evaluation failed")
		}
	}

	return result, nil
}
