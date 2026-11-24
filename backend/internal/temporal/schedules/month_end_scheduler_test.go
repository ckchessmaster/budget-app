package schedules

import (
	"context"
	"errors"
	"testing"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
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

func TestBuildMonthEndRolloverScheduleOptions(t *testing.T) {
	opts := BuildMonthEndRolloverScheduleOptions()

	if opts.ID != MonthEndRolloverScheduleID {
		t.Fatalf("ID = %q, want %q", opts.ID, MonthEndRolloverScheduleID)
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
	if action.TaskQueue != temporalinternal.TaskQueue {
		t.Fatalf("TaskQueue = %q, want %q", action.TaskQueue, temporalinternal.TaskQueue)
	}
}

func TestEnsureMonthEndRolloverSchedule(t *testing.T) {
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

		if err := EnsureMonthEndRolloverSchedule(context.Background(), tc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if captured.ID != MonthEndRolloverScheduleID {
			t.Fatalf("created schedule ID = %q, want %q", captured.ID, MonthEndRolloverScheduleID)
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

		if err := EnsureMonthEndRolloverSchedule(context.Background(), tc); err != nil {
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

		err := EnsureMonthEndRolloverSchedule(context.Background(), tc)
		if !errors.Is(err, boom) {
			t.Fatalf("expected %v, got %v", boom, err)
		}
	})
}
