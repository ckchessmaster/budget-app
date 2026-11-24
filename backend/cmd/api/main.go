package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/server"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
)

func main() {
	cfg := server.LoadConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to parse database configuration: %v", err)
	}
	poolConfig.MaxConns = int32(cfg.DBMaxConns)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		log.Fatalf("failed to initialize database connection pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Printf("warning: postgres ping failed: %v", err)
	} else {
		fmt.Println("Connected to PostgreSQL successfully.")
	}

	// Temporal and the vault master key are required dependencies, not optional extras.
	// Starting without either produces an API that looks healthy while bank sync and any
	// stored SimpleFIN credential are dead, which is exactly how this shipped broken once.
	vault, err := crypto.LoadVaultFromEnv()
	if err != nil {
		log.Fatalf("refusing to start without a vault master key: %v", err)
	}

	temporalClient, err := temporalinternal.Prepare(ctx, slog.Default())
	if err != nil {
		log.Fatalf("refusing to start without Temporal: %v", err)
	}
	defer temporalClient.Close()

	verifier := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
		IssuerURL:       cfg.OIDCIssuerURL,
		ClientID:        cfg.OIDCClientID,
		EnforceAudience: cfg.OIDCEnforceAudience,
	})
	if err := verifier.Warm(ctx); err != nil {
		log.Printf("warning: oidc verifier not ready yet: %v", err)
	}
	srv := server.NewServer(cfg, pool, verifier, temporalClient, vault)

	if err := srv.Run(); err != nil {
		log.Fatalf("server terminated with error: %v", err)
	}
}
