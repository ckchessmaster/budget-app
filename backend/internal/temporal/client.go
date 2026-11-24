package temporal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	durationpb "google.golang.org/protobuf/types/known/durationpb"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

const (
	// TaskQueue is the single queue budget-app's sync workflows and activities run on.
	TaskQueue = "budget-sync-queue"

	// DefaultNamespace keeps budget-app off the shared "default" namespace. Every other
	// workload on the cluster owns a namespace, and a namespace is the only boundary
	// Temporal offers: workflows, schedules and history from another app must never
	// share a queue or visibility store with banking data.
	DefaultNamespace = "budget-app"

	defaultAddress   = "localhost:7233"
	defaultRetention = 72 * time.Hour
)

func AddressFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("TEMPORAL_ADDRESS")); addr != "" {
		return addr
	}
	return defaultAddress
}

func NamespaceFromEnv() string {
	if ns := strings.TrimSpace(os.Getenv("TEMPORAL_NAMESPACE")); ns != "" {
		return ns
	}
	return DefaultNamespace
}

// RetentionFromEnv is how long completed workflow history stays queryable.
func RetentionFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("TEMPORAL_RETENTION"))
	if raw == "" {
		return defaultRetention
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("ignoring invalid TEMPORAL_RETENTION; keeping default",
			"value", raw, "default", defaultRetention.String())
		return defaultRetention
	}
	return d
}

// EnsureNamespace registers the configured namespace when it does not exist yet.
//
// Both the API and the worker call this, so whichever starts first creates it and the
// other tolerates the collision. Registering here rather than requiring an operator
// step means a misconfigured or missing namespace fails loudly at boot instead of
// stranding workflow starts in a namespace nobody can see.
func EnsureNamespace(ctx context.Context, namespace string, retention time.Duration) error {
	c, err := client.Dial(client.Options{HostPort: AddressFromEnv()})
	if err != nil {
		return fmt.Errorf("temporal unreachable at %s: %w", AddressFromEnv(), err)
	}
	defer c.Close()

	_, err = c.WorkflowService().RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        namespace,
		WorkflowExecutionRetentionPeriod: durationpb.New(retention),
	})
	if err == nil {
		slog.Info("registered temporal namespace", "namespace", namespace, "retention", retention.String())
		return nil
	}
	var alreadyExists *serviceerror.NamespaceAlreadyExists
	if errors.As(err, &alreadyExists) {
		return describeNamespace(ctx, c, namespace)
	}
	// Some deployments forbid registration; an existing namespace is still fine.
	if describeErr := describeNamespace(ctx, c, namespace); describeErr == nil {
		return nil
	}
	return fmt.Errorf("temporal namespace %q is unavailable: %w", namespace, err)
}

func describeNamespace(ctx context.Context, c client.Client, namespace string) error {
	_, err := c.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("temporal namespace %q is unavailable: %w", namespace, err)
	}
	return nil
}

// NewClient dials the configured server and namespace.
func NewClient(logger *slog.Logger) (client.Client, error) {
	c, err := client.Dial(client.Options{
		HostPort:  AddressFromEnv(),
		Namespace: NamespaceFromEnv(),
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("temporal unreachable at %s (namespace %q): %w",
			AddressFromEnv(), NamespaceFromEnv(), err)
	}
	return c, nil
}

// Prepare makes sure the namespace exists and returns a connected client. Callers at
// process start treat an error as fatal: Temporal is a required dependency, so a
// server that cannot be reached or a namespace that cannot be resolved must stop the
// process instead of letting it serve traffic with dead bank sync.
func Prepare(ctx context.Context, logger *slog.Logger) (client.Client, error) {
	namespace := NamespaceFromEnv()
	if err := EnsureNamespace(ctx, namespace, RetentionFromEnv()); err != nil {
		return nil, err
	}

	c, err := NewClient(logger)
	if err != nil {
		return nil, err
	}
	if err := HealthCheck(ctx, c); err != nil {
		c.Close()
		return nil, err
	}

	logger.Info("temporal connected",
		"address", AddressFromEnv(), "namespace", namespace, "task_queue", TaskQueue)
	return c, nil
}

// HealthCheck reports whether the server and namespace are reachable right now.
func HealthCheck(ctx context.Context, c client.Client) error {
	if c == nil {
		return errors.New("temporal client is not configured")
	}
	return describeNamespace(ctx, c, NamespaceFromEnv())
}

// SyncScope carries the identity a workflow runs on behalf of.
//
// Row-level security in this database is scoped by app.current_user_id, and every
// tenant table carries FORCE ROW LEVEL SECURITY. A worker that opens a plain pool
// connection therefore sees zero rows and writes nothing, while still reporting a
// successful sync. Activities must scope their transaction the same way the API does,
// which is why the user id travels with the workspace id instead of being looked up.
type SyncScope struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	UserID      uuid.UUID `json:"user_id"`
}
