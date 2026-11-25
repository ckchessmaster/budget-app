package workflows

import (
	"testing"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

// TestDemoResetWorkflowOrdersEnableBeforeDeleteAndSeed pins the sequencing. It is not
// stylistic: the workspace policies hide a demo workspace until demo_mode_enabled is true,
// so a delete or seed issued first cannot see the rows it needs and quietly does nothing.
func TestDemoResetWorkflowOrdersEnableBeforeDeleteAndSeed(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}
	var order []string
	var demoActivities *activities.DemoActivities

	env.OnActivity(demoActivities.EnableDemoModeActivity, mock.Anything, scope).
		Run(func(mock.Arguments) { order = append(order, "enable") }).Return(nil)
	env.OnActivity(demoActivities.CancelWorkspaceSyncsActivity, mock.Anything, scope).
		Run(func(mock.Arguments) { order = append(order, "cancel") }).Return(1, nil)
	env.OnActivity(demoActivities.DeleteDemoSandboxActivity, mock.Anything, scope).
		Run(func(mock.Arguments) { order = append(order, "delete") }).Return(1, nil)

	var seededSandbox uuid.UUID
	env.OnActivity(demoActivities.SeedDemoSandboxActivity, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			order = append(order, "seed")
			in, ok := args.Get(1).(activities.SeedDemoSandboxInput)
			if !ok {
				t.Fatalf("seed activity input = %T, want activities.SeedDemoSandboxInput", args.Get(1))
			}
			seededSandbox = in.SandboxID
		}).
		Return(&activities.SeedDemoSandboxResult{
			WorkspaceID:      uuid.NewString(),
			CategoriesSeeded: 11,
			AccountsSeeded:   4,
			ConnectionReady:  true,
		}, nil)

	env.ExecuteWorkflow(DemoResetWorkflow, DemoResetInput{SyncScope: scope})
	if !env.IsWorkflowCompleted() {
		t.Fatalf("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	want := []string{"enable", "cancel", "delete", "seed"}
	if len(order) != len(want) {
		t.Fatalf("activity order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("activity order = %v, want %v", order, want)
		}
	}
	if seededSandbox == uuid.Nil {
		t.Fatalf("the workflow must hand the seeder a deterministic sandbox id")
	}

	var result DemoResetResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.ConnectionReady || result.CategoriesSeeded != 11 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.SyncsCancelled != 1 {
		t.Fatalf("SyncsCancelled = %d, want 1", result.SyncsCancelled)
	}
}

// TestDemoResetWorkflowDoesNotSyncUnlessAsked keeps the follow-up sync opt-in. A reset that
// always synced would spend a demo bank round trip on every rebuild.
func TestDemoResetWorkflowDoesNotSyncUnlessAsked(t *testing.T) {
	scope := temporalinternal.SyncScope{WorkspaceID: uuid.New(), UserID: uuid.New()}

	run := func(triggerSync bool) int {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		var demoActivities *activities.DemoActivities

		env.OnActivity(demoActivities.EnableDemoModeActivity, mock.Anything, mock.Anything).Return(nil)
		env.OnActivity(demoActivities.CancelWorkspaceSyncsActivity, mock.Anything, mock.Anything).Return(0, nil)
		env.OnActivity(demoActivities.DeleteDemoSandboxActivity, mock.Anything, mock.Anything).Return(0, nil)
		env.OnActivity(demoActivities.SeedDemoSandboxActivity, mock.Anything, mock.Anything).
			Return(&activities.SeedDemoSandboxResult{WorkspaceID: scope.WorkspaceID.String(), ConnectionReady: true}, nil)
		starts := 0
		env.OnActivity(demoActivities.StartSandboxSyncActivity, mock.Anything, mock.Anything).
			Run(func(mock.Arguments) { starts++ }).Return(nil)

		env.ExecuteWorkflow(DemoResetWorkflow, DemoResetInput{SyncScope: scope, TriggerSync: triggerSync})
		if !env.IsWorkflowCompleted() {
			t.Fatalf("workflow did not complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("workflow failed: %v", err)
		}
		return starts
	}

	if got := run(false); got != 0 {
		t.Fatalf("reset without trigger_sync started %d syncs, want 0", got)
	}
	if got := run(true); got != 1 {
		t.Fatalf("reset with trigger_sync started %d syncs, want 1", got)
	}
}
