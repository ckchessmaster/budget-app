package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkspaceRole string

const (
	RoleOwner    WorkspaceRole = "owner"
	RoleMember   WorkspaceRole = "member"
	RoleReadOnly WorkspaceRole = "read_only"
)

const WorkspaceContextKey ContextKey = "workspace_context"

var ErrWorkspaceNotFound = errors.New("workspace_context not found in context")

type WorkspaceContext struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        WorkspaceRole
	IsDemo      bool
}

func WithWorkspaceContext(ctx context.Context, wc WorkspaceContext) context.Context {
	return context.WithValue(ctx, WorkspaceContextKey, wc)
}

func GetWorkspaceContext(ctx context.Context) (WorkspaceContext, error) {
	val, ok := ctx.Value(WorkspaceContextKey).(WorkspaceContext)
	if !ok || val.WorkspaceID == uuid.Nil {
		return WorkspaceContext{}, ErrWorkspaceNotFound
	}
	return val, nil
}

// RequireWorkspace resolves the X-Workspace-ID header (or falls back to the user's
// owned/first workspace matching demo_mode_enabled), verifies active membership in
// workspace_members, lazily ensures owner/roster consistency, and injects both
// WorkspaceContext and db.WithWorkspaceID onto the request context.
func RequireWorkspace(pool *pgxpool.Pool, provisioner workspace.Provisioner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := GetUserID(r.Context())
			if err != nil {
				writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			var requestedWS uuid.UUID
			if rawHeader := strings.TrimSpace(r.Header.Get("X-Workspace-ID")); rawHeader != "" {
				parsed, parseErr := uuid.Parse(rawHeader)
				if parseErr != nil || parsed == uuid.Nil {
					writeErrorJSON(w, http.StatusBadRequest, "invalid_workspace_id")
					return
				}
				requestedWS = parsed
			}

			var (
				resolved       WorkspaceContext
				forbidden      bool
				needsProvision bool
			)

			// Lookup transaction intentionally omits app.current_workspace_id so it can
			// inspect the caller's memberships and validate X-Workspace-ID.
			lookupCtx := r.Context()
			email := GetUserEmail(lookupCtx)
			displayName := GetUserName(lookupCtx)

			err = db.WithWorkspaceTx(lookupCtx, pool, userID, func(tx pgx.Tx) error {
				var demoEnabled bool
				if err := tx.QueryRow(lookupCtx, `
					SELECT COALESCE(
						(SELECT demo_mode_enabled FROM user_preferences WHERE user_id = $1),
						FALSE
					)
				`, userID).Scan(&demoEnabled); err != nil {
					return err
				}

				if requestedWS != uuid.Nil {
					var roleStr string
					err := tx.QueryRow(lookupCtx, `
						SELECT role FROM workspace_members
						WHERE workspace_id = $1 AND user_id = $2
					`, requestedWS, userID).Scan(&roleStr)
					if errors.Is(err, pgx.ErrNoRows) {
						forbidden = true
						return nil
					}
					if err != nil {
						return err
					}

					var isDemo bool
					err = tx.QueryRow(lookupCtx, `
						SELECT is_demo FROM workspaces WHERE id = $1
					`, requestedWS).Scan(&isDemo)
					if err == nil {
						resolved = WorkspaceContext{
							WorkspaceID: requestedWS,
							UserID:      userID,
							Role:        WorkspaceRole(roleStr),
							IsDemo:      isDemo,
						}
					} else if !errors.Is(err, pgx.ErrNoRows) {
						return err
					}
					// If ErrNoRows here, requestedWS belongs to the user but its is_demo does not
					// match demo_mode_enabled (e.g. stale localStorage header after toggling Demo Mode).
					// Fall through to resolve the active workspace matching demo_mode_enabled.
				}

				if resolved.WorkspaceID == uuid.Nil {
					var wsID uuid.UUID
					var roleStr string
					var isDemo bool
					err := tx.QueryRow(lookupCtx, `
						SELECT w.id, wm.role, w.is_demo
						FROM workspaces w
						JOIN workspace_members wm ON wm.workspace_id = w.id
						WHERE wm.user_id = $1 AND w.is_demo = $2
						ORDER BY (wm.role = 'owner') DESC, w.created_at ASC
						LIMIT 1
					`, userID, demoEnabled).Scan(&wsID, &roleStr, &isDemo)
					if errors.Is(err, pgx.ErrNoRows) {
						if demoEnabled {
							// Stale demo preference with missing demo workspace -> fall back to real workspace.
							if _, uErr := tx.Exec(lookupCtx, `
								UPDATE user_preferences SET demo_mode_enabled = FALSE, updated_at = NOW() WHERE user_id = $1
							`, userID); uErr != nil {
								return uErr
							}
							err = tx.QueryRow(lookupCtx, `
								SELECT w.id, wm.role, w.is_demo
								FROM workspaces w
								JOIN workspace_members wm ON wm.workspace_id = w.id
								WHERE wm.user_id = $1 AND w.is_demo = FALSE
								ORDER BY (wm.role = 'owner') DESC, w.created_at ASC
								LIMIT 1
							`, userID).Scan(&wsID, &roleStr, &isDemo)
						}
						if errors.Is(err, pgx.ErrNoRows) {
							needsProvision = true
							return nil
						}
					}
					if err != nil {
						return err
					}
					resolved = WorkspaceContext{
						WorkspaceID: wsID,
						UserID:      userID,
						Role:        WorkspaceRole(roleStr),
						IsDemo:      isDemo,
					}
				}

				return syncWorkspaceOwnershipAndRosterTx(lookupCtx, tx, resolved, email, displayName)
			})
			if err != nil {
				writeErrorJSON(w, http.StatusInternalServerError, "failed_to_resolve_workspace")
				return
			}
			if forbidden {
				writeErrorJSON(w, http.StatusForbidden, "workspace_forbidden")
				return
			}

			if needsProvision {
				if provisioner == nil {
					writeErrorJSON(w, http.StatusNotFound, "workspace_not_found")
					return
				}
				prov, pErr := provisioner.EnsureWorkspaceProvisioned(lookupCtx, pool, userID)
				if pErr != nil {
					writeErrorJSON(w, http.StatusInternalServerError, "failed_to_provision_workspace")
					return
				}
				resolved = WorkspaceContext{
					WorkspaceID: prov.WorkspaceID,
					UserID:      userID,
					Role:        WorkspaceRole(prov.Role),
					IsDemo:      prov.IsDemo,
				}
				if resolved.Role == "" {
					resolved.Role = RoleOwner
				}
				if email != "" || displayName != "" {
					_ = db.WithWorkspaceTx(db.WithWorkspaceID(lookupCtx, resolved.WorkspaceID), pool, userID, func(tx pgx.Tx) error {
						return syncWorkspaceOwnershipAndRosterTx(lookupCtx, tx, resolved, email, displayName)
					})
				}
			}

			ctx := WithWorkspaceContext(r.Context(), resolved)
			ctx = db.WithWorkspaceID(ctx, resolved.WorkspaceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func syncWorkspaceOwnershipAndRosterTx(
	ctx context.Context,
	tx pgx.Tx,
	wc WorkspaceContext,
	email string,
	displayName string,
) error {
	if wc.Role == RoleOwner {
		if _, err := tx.Exec(ctx, `
			UPDATE workspaces SET owner_user_id = $2
			WHERE id = $1 AND owner_user_id IS NULL
		`, wc.WorkspaceID, wc.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO workspace_owners (workspace_id, owner_user_id)
			VALUES ($1, $2)
			ON CONFLICT (workspace_id) DO NOTHING
		`, wc.WorkspaceID, wc.UserID); err != nil {
			return err
		}
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO workspace_roster (workspace_id, user_id, role, email, display_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, user_id) DO UPDATE
		SET role = EXCLUDED.role,
		    email = CASE WHEN EXCLUDED.email <> '' THEN EXCLUDED.email ELSE workspace_roster.email END,
		    display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE workspace_roster.display_name END
	`, wc.WorkspaceID, wc.UserID, string(wc.Role), email, displayName)
	return err
}

// RequireRole enforces role-based access control on route groups.
func RequireRole(allowed ...WorkspaceRole) func(http.Handler) http.Handler {
	allowedSet := make(map[WorkspaceRole]struct{}, len(allowed))
	for _, role := range allowed {
		allowedSet[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := GetUserID(r.Context()); err != nil {
				writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			wc, err := GetWorkspaceContext(r.Context())
			if err != nil {
				writeErrorJSON(w, http.StatusForbidden, "forbidden")
				return
			}
			if _, ok := allowedSet[wc.Role]; !ok {
				writeErrorJSON(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
