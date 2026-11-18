package main

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/activities"
	"github.com/ckchessmaster/budget-app/backend/internal/temporal/workflows"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func loadVault() crypto.Vault {
	if b64 := os.Getenv("VAULT_MASTER_KEY"); b64 != "" {
		if v, err := crypto.NewAESGCMVaultFromBase64(b64); err == nil {
			return v
		}
	}
	sum := sha256.Sum256([]byte("budget-app-dev-vault-key-change-in-production"))
	v, _ := crypto.NewAESGCMVault(sum[:])
	return v
}

func dialTemporalWithRetry(ctx context.Context, logger *slog.Logger) (client.Client, error) {
	for {
		c, err := temporalinternal.NewClient(logger)
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

	w := worker.New(c, temporalinternal.TaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.BankSyncWorkflow)
	w.RegisterActivity(&activities.BankActivities{
		Pool:   pool,
		Client: simplefin.NewClient(nil),
		Vault:  loadVault(),
	})

	logger.Info("starting Temporal worker", "task_queue", temporalinternal.TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("Temporal worker stopped with error", "error", err)
		os.Exit(1)
	}
}
