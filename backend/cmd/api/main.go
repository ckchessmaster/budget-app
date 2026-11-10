package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/internal/middleware"
	"github.com/ckchessmaster/budget-app/backend/internal/server"
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

	verifier := middleware.NewJWTVerifier(cfg.DexIssuerURL)
	srv := server.NewServer(cfg, pool, verifier)

	if err := srv.Run(); err != nil {
		log.Fatalf("server terminated with error: %v", err)
	}
}
