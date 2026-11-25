package workflows

import (
	"errors"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

func TestBankSyncWorkflow_Unit(t *testing.T) {
	t.Run("executes full pipeline including notification evaluation", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()

		scope := temporalinternal.SyncScope{
			WorkspaceID: uuid.New(),
			UserID:      uuid.New(),
		}
		sfData := &simplefin.DataResponse{}

		var acts *activities.BankActivities
		env.OnActivity(acts.FetchSimpleFINDataActivity, mock.Anything, scope).Return(sfData, nil).Once()
		env.OnActivity(acts.PersistTransactionsActivity, mock.Anything, activities.PersistTransactionsInput{
			SyncScope: scope,
			Data:      sfData,
		}).Return(activities.PersistTransactionsResult{
			FetchedCount:  4,
			InsertedCount: 3,
			SettledCount:  1,
		}, nil).Once()
		env.OnActivity(acts.DetectAndLinkTransfersActivity, mock.Anything, scope).Return(1, nil).Once()
		env.OnActivity(acts.NormalizeMerchantsActivity, mock.Anything, scope).Return(3, nil).Once()
		env.OnActivity(acts.EvaluateNotificationsActivity, mock.Anything, scope).Return(notifications.EvaluationResult{
			SpendingAlertsSent:  1,
			StatementAlertsSent: 1,
		}, nil).Once()

		env.ExecuteWorkflow(BankSyncWorkflow, BankSyncInput{SyncScope: scope})
		if !env.IsWorkflowCompleted() {
			t.Fatal("expected BankSyncWorkflow to complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("unexpected workflow error: %v", err)
		}

		var res BankSyncResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("failed to decode workflow result: %v", err)
		}
		if res.FetchedCount != 4 || res.InsertedCount != 3 || res.SettledCount != 1 || res.TransfersLinked != 1 || res.NormalizedCount != 3 || res.NotificationsSent != 2 {
			t.Fatalf("unexpected BankSyncResult: %+v", res)
		}
		env.AssertExpectations(t)
	})

	t.Run("notification evaluation failure degrades to warning without failing sync", func(t *testing.T) {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()

		scope := temporalinternal.SyncScope{
			WorkspaceID: uuid.New(),
			UserID:      uuid.New(),
		}
		sfData := &simplefin.DataResponse{}

		var acts *activities.BankActivities
		env.OnActivity(acts.FetchSimpleFINDataActivity, mock.Anything, scope).Return(sfData, nil).Once()
		env.OnActivity(acts.PersistTransactionsActivity, mock.Anything, activities.PersistTransactionsInput{
			SyncScope: scope,
			Data:      sfData,
		}).Return(activities.PersistTransactionsResult{
			FetchedCount:  2,
			InsertedCount: 2,
			Warnings:      []string{"upstream minor warning"},
		}, nil).Once()
		env.OnActivity(acts.DetectAndLinkTransfersActivity, mock.Anything, scope).Return(0, nil).Once()
		env.OnActivity(acts.NormalizeMerchantsActivity, mock.Anything, scope).Return(2, nil).Once()
		env.OnActivity(acts.EvaluateNotificationsActivity, mock.Anything, scope).Return(notifications.EvaluationResult{}, errors.New("push gateway down")).Times(3)

		env.ExecuteWorkflow(BankSyncWorkflow, BankSyncInput{SyncScope: scope})
		if !env.IsWorkflowCompleted() {
			t.Fatal("expected BankSyncWorkflow to complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("expected non-fatal notification failure, got error: %v", err)
		}

		var res BankSyncResult
		if err := env.GetWorkflowResult(&res); err != nil {
			t.Fatalf("failed to decode workflow result: %v", err)
		}
		if res.NotificationsSent != 0 {
			t.Fatalf("NotificationsSent = %d, want 0", res.NotificationsSent)
		}
		if len(res.Warnings) != 2 || res.Warnings[0] != "upstream minor warning" || res.Warnings[1] != "notification evaluation failed" {
			t.Fatalf("unexpected warnings: %+v", res.Warnings)
		}
		env.AssertExpectations(t)
	})
}
