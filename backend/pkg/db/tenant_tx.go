package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TxFunc func(tx pgx.Tx) error

// WithWorkspaceTx executes fn inside a database transaction scoped with Row-Level Security
// by injecting the authenticated user's ID into app.current_user_id.
// It rolls back automatically if fn returns an error or panics.
func WithWorkspaceTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn TxFunc) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		} else if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Inject RLS context parameter for tenant isolation (transaction-local)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String()); err != nil {
		return fmt.Errorf("failed to set RLS context for user %s: %w", userID, err)
	}

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
