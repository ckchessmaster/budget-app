package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/demo"
	"github.com/ckchessmaster/budget-app/backend/internal/notifications"
	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func dialTemporalWithRetry(ctx context.Context, logger *slog.Logger) (client.Client, error) {
	for {
		c, err := temporalinternal.Prepare(ctx, logger)
		if err == nil {
			return c, nil
		}
		logger.Warn("failed to connect to Temporal, retrying in 5s", "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	vault, err := crypto.LoadVaultFromEnv()
	if err != nil {
		logger.Error("temporal worker requires a vault master key; refusing to start with a derived key",
			"error", err)
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	c, err := dialTemporalWithRetry(ctx, logger)
	if err != nil {
		logger.Error("worker shutting down before Temporal connection", "error", err)
		os.Exit(1)
	}
	defer c.Close()

	vapidCfg, err := notifications.LoadVAPIDConfigFromEnv()
	if err != nil {
		logger.Error("invalid or partial VAPID configuration; web push notifications disabled", "error", err)
	}
	pushSvc := notifications.NewWebPushService(pool, vapidCfg, nil)
	notifEval := notifications.NewEvaluator(pool, pushSvc)

	w := worker.New(c, temporalinternal.TaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.BankSyncWorkflow)
	w.RegisterWorkflow(workflows.MonthEndRolloverWorkflow)
	w.RegisterWorkflow(workflows.SimpleFINClaimWorkflow)
	w.RegisterWorkflow(workflows.DemoResetWorkflow)
	w.RegisterActivity(&activities.BankActivities{
		Pool:                  pool,
		Client:                simplefin.NewClient(nil),
		Vault:                 vault,
		NotificationEvaluator: notifEval,
	})
	w.RegisterActivity(&activities.RolloverActivities{
		Pool: pool,
	})
	w.RegisterActivity(&activities.ClaimActivities{
		Pool:   pool,
		Client: simplefin.NewClient(nil),
		Vault:  vault,
	})
	// The worker builds demo access URLs, so it needs the same bridge address the API
	// dials. The chart sets this in the shared ConfigMap; the fallback only matters for a
	// local run where the worker and the API share a host.
	mockBaseURL := os.Getenv("MOCK_SIMPLEFIN_BASE_URL")
	if mockBaseURL == "" {
		mockBaseURL = demo.DefaultMockBridgeBaseURL
	}
	w.RegisterActivity(&activities.DemoActivities{
		Pool:        pool,
		Vault:       vault,
		MockBaseURL: mockBaseURL,
		Temporal:    c,
		SeedSandbox: demo.SeedDemoWorkspaceTxWithID,
	})

	logger.Info("starting Temporal worker", "task_queue", temporalinternal.TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("Temporal worker stopped with error", "error", err)
		os.Exit(1)
	}
}
