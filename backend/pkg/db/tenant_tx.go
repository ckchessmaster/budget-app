package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TxFunc func(tx pgx.Tx) error

type workspaceIDContextKey struct{}

// WithWorkspaceID attaches an active workspace ID to ctx so BeginWorkspaceTx /
// WithWorkspaceTx automatically inject app.current_workspace_id into PostgreSQL.
func WithWorkspaceID(ctx context.Context, workspaceID uuid.UUID) context.Context {
	if workspaceID == uuid.Nil {
		return ctx
	}
	return context.WithValue(ctx, workspaceIDContextKey{}, workspaceID)
}

// WorkspaceIDFromContext returns the active workspace ID attached by WithWorkspaceID, if any.
func WorkspaceIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if ctx == nil {
		return uuid.Nil, false
	}
	val, ok := ctx.Value(workspaceIDContextKey{}).(uuid.UUID)
	if !ok || val == uuid.Nil {
		return uuid.Nil, false
	}
	return val, true
}

// BeginWorkspaceTx starts a transaction scoped with Row-Level Security by injecting the
// authenticated user's ID into app.current_user_id and, when present on ctx, the active
// workspace ID into app.current_workspace_id.
//
// Callers that manage commit and rollback by hand must use this instead of pool.Begin.
// Tenant tables carry FORCE ROW LEVEL SECURITY, so an unscoped transaction sees no tenant
// rows at all and every write is rejected by the policies' WITH CHECK clauses, which
// surface as "new row violates row-level security policy" rather than as an auth error.
//
// The caller owns the returned transaction and must commit or roll it back.
func BeginWorkspaceTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Inject RLS context parameters for tenant isolation (transaction-local)
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_user_id', $1, true)", userID.String()); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("failed to set RLS context for user %s: %w", userID, err)
	}

	if wsID, ok := WorkspaceIDFromContext(ctx); ok {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.current_workspace_id', $1, true)", wsID.String()); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("failed to set RLS workspace context %s: %w", wsID, err)
		}
	}

	return tx, nil
}

// BeginInviteTokenTx starts a transaction scoped to a single SHA-256 invitation token hash
// via app.invite_token_hash so public preview/accept routes can inspect the matching
// invitation under FORCE ROW LEVEL SECURITY without bypassing RLS.
func BeginInviteTokenTx(ctx context.Context, pool *pgxpool.Pool, tokenHash string) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin invite transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.invite_token_hash', $1, true)", tokenHash); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("failed to set invite token RLS context: %w", err)
	}
	return tx, nil
}

// WithWorkspaceTx executes fn inside a database transaction scoped with Row-Level Security
// by injecting the authenticated user's ID into app.current_user_id (and optional
// app.current_workspace_id from ctx). It rolls back automatically if fn returns an error or panics.
func WithWorkspaceTx(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn TxFunc) (err error) {
	tx, err := BeginWorkspaceTx(ctx, pool, userID)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		} else if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ActiveWorkspaceID resolves the active workspace ID for the transaction's
// app.current_workspace_id / app.current_user_id settings, matching user_preferences.demo_mode_enabled.
// Filtering explicitly by workspace_id protects queries even when connected as a DB superuser.
func ActiveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	if wsID, ok := WorkspaceIDFromContext(ctx); ok {
		var verified uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT w.id
			FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE w.id = $1
			  AND wm.user_id = current_app_user_id()
			  AND w.is_demo = COALESCE(
			      (SELECT up.demo_mode_enabled FROM user_preferences up WHERE up.user_id = current_app_user_id()),
			      FALSE
			  )
		`, wsID).Scan(&verified)
		if err == nil {
			return verified, nil
		}
	}

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
		ORDER BY (wm.role = 'owner') DESC, w.created_at ASC
		LIMIT 1
	`).Scan(&workspaceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve active workspace_id: %w", err)
	}
	return workspaceID, nil
}
