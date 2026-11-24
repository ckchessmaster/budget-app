package schedules

import (
	"context"
	"errors"
	"fmt"
	"testing"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

type stubScheduleClient struct {
	client.ScheduleClient
	createFn func(ctx context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error)
}

func (s *stubScheduleClient) Create(ctx context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error) {
	return s.createFn(ctx, options)
}

type stubTemporalClient struct {
	client.Client
	schedClient client.ScheduleClient
}

func (c *stubTemporalClient) ScheduleClient() client.ScheduleClient {
	return c.schedClient
}

func TestBuildWorkspaceRolloverScheduleOptions(t *testing.T) {
	scope := temporalinternal.SyncScope{
		WorkspaceID: uuid.New(),
		UserID:      uuid.New(),
	}
	opts := BuildWorkspaceRolloverScheduleOptions(scope)

	wantScheduleID := fmt.Sprintf("month-end-rollover-%s", scope.WorkspaceID.String())
	if opts.ID != wantScheduleID {
		t.Fatalf("ID = %q, want %q", opts.ID, wantScheduleID)
	}
	if len(opts.Spec.CronExpressions) != 1 || opts.Spec.CronExpressions[0] != "0 0 1 * *" {
		t.Fatalf("CronExpressions = %v, want [0 0 1 * *]", opts.Spec.CronExpressions)
	}
	if opts.Spec.TimeZoneName != "UTC" {
		t.Fatalf("TimeZoneName = %q, want UTC", opts.Spec.TimeZoneName)
	}
	if opts.Overlap != enumspb.SCHEDULE_OVERLAP_POLICY_SKIP {
		t.Fatalf("Overlap = %v, want SCHEDULE_OVERLAP_POLICY_SKIP", opts.Overlap)
	}

	action, ok := opts.Action.(*client.ScheduleWorkflowAction)
	if !ok {
		t.Fatalf("expected *client.ScheduleWorkflowAction, got %T", opts.Action)
	}
	wantWFID := fmt.Sprintf("month-end-rollover-wf-%s", scope.WorkspaceID.String())
	if action.ID != wantWFID {
		t.Fatalf("Action.ID = %q, want %q", action.ID, wantWFID)
	}
	if action.TaskQueue != temporalinternal.TaskQueue {
		t.Fatalf("TaskQueue = %q, want %q", action.TaskQueue, temporalinternal.TaskQueue)
	}
	if len(action.Args) != 1 {
		t.Fatalf("Args len = %d, want 1", len(action.Args))
	}
	in, ok := action.Args[0].(workflows.MonthEndRolloverInput)
	if !ok {
		t.Fatalf("Args[0] type = %T, want workflows.MonthEndRolloverInput", action.Args[0])
	}
	if in.SyncScope != scope {
		t.Fatalf("Args[0].SyncScope = %+v, want %+v", in.SyncScope, scope)
	}
}

func TestEnsureWorkspaceRolloverSchedule(t *testing.T) {
	scope := temporalinternal.SyncScope{
		WorkspaceID: uuid.New(),
		UserID:      uuid.New(),
	}
	wantID := fmt.Sprintf("month-end-rollover-%s", scope.WorkspaceID.String())

	t.Run("creates schedule cleanly", func(t *testing.T) {
		var captured client.ScheduleOptions
		tc := &stubTemporalClient{
			schedClient: &stubScheduleClient{
				createFn: func(_ context.Context, options client.ScheduleOptions) (client.ScheduleHandle, error) {
					captured = options
					return nil, nil
				},
			},
		}

		if err := EnsureWorkspaceRolloverSchedule(context.Background(), tc, scope); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if captured.ID != wantID {
			t.Fatalf("created schedule ID = %q, want %q", captured.ID, wantID)
		}
	})

	t.Run("ignores AlreadyExists", func(t *testing.T) {
		tc := &stubTemporalClient{
			schedClient: &stubScheduleClient{
				createFn: func(_ context.Context, _ client.ScheduleOptions) (client.ScheduleHandle, error) {
					return nil, serviceerror.NewAlreadyExists("schedule already exists")
				},
			},
		}

		if err := EnsureWorkspaceRolloverSchedule(context.Background(), tc, scope); err != nil {
			t.Fatalf("expected nil on AlreadyExists, got %v", err)
		}
	})

	t.Run("propagates non-AlreadyExists error", func(t *testing.T) {
		boom := errors.New("temporal unavailable")
		tc := &stubTemporalClient{
			schedClient: &stubScheduleClient{
				createFn: func(_ context.Context, _ client.ScheduleOptions) (client.ScheduleHandle, error) {
					return nil, boom
				},
			},
		}

		err := EnsureWorkspaceRolloverSchedule(context.Background(), tc, scope)
		if !errors.Is(err, boom) {
			t.Fatalf("expected %v, got %v", boom, err)
		}
	})

	t.Run("rejects missing scope", func(t *testing.T) {
		tc := &stubTemporalClient{}
		if err := EnsureWorkspaceRolloverSchedule(context.Background(), tc, temporalinternal.SyncScope{WorkspaceID: scope.WorkspaceID}); err == nil {
			t.Fatalf("expected error when UserID is missing")
		}
		if err := EnsureWorkspaceRolloverSchedule(context.Background(), tc, temporalinternal.SyncScope{UserID: scope.UserID}); err == nil {
			t.Fatalf("expected error when WorkspaceID is missing")
		}
	})
}
