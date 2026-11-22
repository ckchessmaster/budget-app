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

// ActiveWorkspaceID resolves the active workspace ID for the transaction's
// app.current_user_id setting, matching user_preferences.demo_mode_enabled.
// Filtering explicitly by workspace_id protects queries even when connected as a DB superuser.
func ActiveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var workspaceID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT w.id
		FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.user_id = current_app_user_id()
		  AND w.is_demo = COALESCE(
		      (SELECT up.demo_mode_enabled FROM user_preferences up WHERE up.user_id = current_app_user_id()),
		      FALSE
		  )
		ORDER BY w.created_at ASC
		LIMIT 1
	`).Scan(&workspaceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve active workspace_id: %w", err)
	}
	return workspaceID, nil
}
