package workflows

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func replayBatch(n int) []temporalinternal.ReplayItem {
	items := make([]temporalinternal.ReplayItem, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, temporalinternal.ReplayItem{
			ClientID:        fmt.Sprintf("item-%d", i),
			AccountID:       uuid.New(),
			Amount:          decimal.NewFromFloat(-3.25),
			MerchantRaw:     "Corner Store",
			TransactionDate: "2026-11-18",
		})
	}
	return items
}

func appliedOK(clientID string) (activities.ReplayItemResult, error) {
	return activities.ReplayItemResult{ClientID: clientID, TransactionID: uuid.New()}, nil
}

func mockNotify(env *testsuite.TestWorkflowEnvironment, scope temporalinternal.SyncScope, err error) {
	var bank *activities.BankActivities
	env.OnActivity(bank.EvaluateNotificationsActivity, mock.Anything, scope).
		Return(notifications.EvaluationResult{}, err)
}

func TestOfflineReplayWorkflow_LandsEveryItemButOne(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}
	items := replayBatch(5)

	var replay *activities.ReplayActivities
	for _, item := range items {
		in := activities.ReplayItemInput{SyncScope: scope, Item: item}
		if item.ClientID == "item-2" {
			env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything, in).
				Return(activities.ReplayItemResult{}, temporal.NewNonRetryableApplicationError(
					"the queued account no longer exists in this workspace", "unknown_account", nil)).
				Once()
			continue
		}
		env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything, in).
			Return(appliedOK(item.ClientID)).Once()
	}
	mockNotify(env, scope, nil)

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{SyncScope: scope, Items: items})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	// One bad entry must not fail the batch: the workflow's whole job is to say which
	// items landed and which did not.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	var res OfflineReplayResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(res.Applied) != 4 {
		t.Fatalf("applied = %v, want the four valid items", res.Applied)
	}
	if len(res.Rejected) != 1 {
		t.Fatalf("rejected = %+v, want exactly one", res.Rejected)
	}
	bad := res.Rejected[0]
	if bad.ClientID != "item-2" {
		t.Fatalf("rejected client_id = %q, want item-2", bad.ClientID)
	}
	if bad.Reason != "unknown_account" {
		t.Fatalf("rejected reason = %q, want the machine-readable code", bad.Reason)
	}
	if bad.Retryable {
		t.Fatalf("a validation rejection must be terminal, got retryable")
	}
	for _, clientID := range res.Applied {
		if clientID == "item-2" {
			t.Fatalf("the rejected item appeared in applied")
		}
	}
}

func TestOfflineReplayWorkflow_RetriesTransientFailureWithoutAClientResend(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}
	items := replayBatch(2)

	var replay *activities.ReplayActivities
	// A deadlock is momentary: the workflow retries the item on its own and the browser
	// never hears about it. This is the failure the synchronous loop used to hand back as
	// "failed" and forget.
	first := activities.ReplayItemInput{SyncScope: scope, Item: items[0]}
	second := activities.ReplayItemInput{SyncScope: scope, Item: items[1]}
	env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything, first).
		Return(activities.ReplayItemResult{}, errors.New("deadlock detected")).Once()
	env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything, first).
		Return(appliedOK("item-0")).Once()
	env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything, second).
		Return(appliedOK("item-1")).Once()
	mockNotify(env, scope, nil)

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{SyncScope: scope, Items: items})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	var res OfflineReplayResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(res.Applied) != 2 || len(res.Rejected) != 0 {
		t.Fatalf("applied=%v rejected=%+v, want both items applied", res.Applied, res.Rejected)
	}
	env.AssertNumberOfCalls(t, "ReplayTransactionItemActivity", 3)
}

func TestOfflineReplayWorkflow_ReportsAnExhaustedItemAsRetryable(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}
	items := replayBatch(2)

	var replay *activities.ReplayActivities
	env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything,
		activities.ReplayItemInput{SyncScope: scope, Item: items[0]}).
		Return(activities.ReplayItemResult{}, errors.New("connection reset by peer"))
	env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything,
		activities.ReplayItemInput{SyncScope: scope, Item: items[1]}).
		Return(appliedOK("item-1")).Once()
	mockNotify(env, scope, nil)

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{SyncScope: scope, Items: items})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	var res OfflineReplayResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(res.Applied) != 1 || res.Applied[0] != "item-1" {
		t.Fatalf("applied = %v, want item-1 to survive on its own", res.Applied)
	}
	if len(res.Rejected) != 1 || !res.Rejected[0].Retryable {
		t.Fatalf("rejected = %+v, want item-0 reported as retryable", res.Rejected)
	}
	if res.Rejected[0].Reason != "replay_attempts_exhausted" {
		t.Fatalf("reason = %q, want replay_attempts_exhausted", res.Rejected[0].Reason)
	}
}

func TestOfflineReplayWorkflow_EmptyBatchStartsNothing(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{SyncScope: scope})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	var res OfflineReplayResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	// Non-nil so the client sees [] rather than null and does not have to special-case it.
	if res.Applied == nil || res.Rejected == nil {
		t.Fatalf("result = %+v, want empty lists rather than nulls", res)
	}
	env.AssertNotCalled(t, "ReplayTransactionItemActivity", mock.Anything, mock.Anything)
}

func TestOfflineReplayWorkflow_RejectsAnOversizedBatch(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}
	items := replayBatch(temporalinternal.MaxReplayBatchItems + 1)

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{SyncScope: scope, Items: items})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatalf("expected an oversized batch to be refused")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || appErr.Type() != "batch_too_large" {
		t.Fatalf("error = %v, want a terminal batch_too_large", err)
	}
	env.AssertNotCalled(t, "ReplayTransactionItemActivity", mock.Anything, mock.Anything)
}

func TestOfflineReplayWorkflow_RequiresAWorkspaceScope(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{
		SyncScope: temporalinternal.SyncScope{UserID: uuid.New()},
		Items:     replayBatch(1),
	})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatalf("expected a scope without a workspace to be refused")
	}
	// Without a scope every write would match zero rows and still look like success.
	env.AssertNotCalled(t, "ReplayTransactionItemActivity", mock.Anything, mock.Anything)
}

func TestOfflineReplayWorkflow_NotificationFailureDoesNotLoseAppliedItems(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}
	items := replayBatch(2)

	var replay *activities.ReplayActivities
	for _, item := range items {
		env.OnActivity(replay.ReplayTransactionItemActivity, mock.Anything,
			activities.ReplayItemInput{SyncScope: scope, Item: item}).
			Return(appliedOK(item.ClientID)).Once()
	}
	mockNotify(env, scope, errors.New("push service unavailable"))

	env.ExecuteWorkflow(OfflineReplayWorkflow, OfflineReplayInput{SyncScope: scope, Items: items})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	var res OfflineReplayResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(res.Applied) != 2 {
		t.Fatalf("applied = %v, want both rows kept", res.Applied)
	}
	if len(res.Warnings) == 0 {
		t.Fatalf("expected the failed alert pass to be reported as a warning")
	}
}
