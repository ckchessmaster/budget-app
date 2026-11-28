package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

var (
	ErrAlreadyOwnsWorkspace = errors.New("user already owns a personal workspace")
	ErrInvalidWorkspaceName = errors.New("workspace name is required")
)

type ProvisionResult struct {
	WorkspaceID        uuid.UUID `json:"workspace_id"`
	WorkspaceName      string    `json:"workspace_name"`
	Role               string    `json:"role"`
	IsDemo             bool      `json:"is_demo"`
	DemoModeEnabled    bool      `json:"demo_mode_enabled"`
	InboundReceiptMail string    `json:"inbound_receipt_mail"`
	IsNewUser          bool      `json:"is_new_user"`
}

type WorkspaceSummary struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Role   string    `json:"role"`
	IsDemo bool      `json:"is_demo"`
}

type Provisioner interface {
	EnsureWorkspaceProvisioned(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*ProvisionResult, error)
}

type DefaultProvisioner struct {
	ReceiptDomain string
}

func NewProvisioner() *DefaultProvisioner {
	domain := os.Getenv("RECEIPT_DOMAIN")
	if domain == "" {
		domain = "receipts.yourdomain.com"
	}
	return &DefaultProvisioner{
		ReceiptDomain: domain,
	}
}

func (p *DefaultProvisioner) EnsureWorkspaceProvisioned(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (*ProvisionResult, error) {
	var result *ProvisionResult
	err := db.WithWorkspaceTx(ctx, pool, userID, func(tx pgx.Tx) error {
		var demoEnabled bool
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(
				(SELECT demo_mode_enabled FROM user_preferences WHERE user_id = $1),
				FALSE
			)
		`, userID).Scan(&demoEnabled)
		if err != nil {
			return fmt.Errorf("failed to check user demo preference: %w", err)
		}

		if demoEnabled {
			var workspaceID uuid.UUID
			var workspaceName string
			var role string
			err = tx.QueryRow(ctx, `
				SELECT w.id, w.name, wm.role
				FROM workspaces w
				JOIN workspace_members wm ON wm.workspace_id = w.id
				WHERE wm.user_id = $1 AND w.is_demo = TRUE
				ORDER BY w.created_at ASC
				LIMIT 1
			`, userID).Scan(&workspaceID, &workspaceName, &role)
			if err == nil {
				var inboundReceiptAlias string
				err = tx.QueryRow(ctx, `
					SELECT inbound_receipt_alias FROM workspace_settings
					WHERE workspace_id = $1
				`, workspaceID).Scan(&inboundReceiptAlias)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("failed to fetch demo workspace settings: %w", err)
				}
				result = &ProvisionResult{
					WorkspaceID:        workspaceID,
					WorkspaceName:      workspaceName,
					Role:               role,
					IsDemo:             true,
					DemoModeEnabled:    true,
					InboundReceiptMail: inboundReceiptAlias,
					IsNewUser:          false,
				}
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("failed to check existing demo workspace: %w", err)
			}
			// Demo workspace missing while preference was enabled; reset preference to real workspace.
			if _, err := tx.Exec(ctx, `
				UPDATE user_preferences SET demo_mode_enabled = FALSE, updated_at = NOW() WHERE user_id = $1
			`, userID); err != nil {
				return fmt.Errorf("failed to reset stale demo preference: %w", err)
			}
		}

		// 1. Check if caller requested a specific workspace or already belongs to a real workspace.
		var workspaceID uuid.UUID
		var workspaceName string
		var role string
		if requestedWS, ok := db.WorkspaceIDFromContext(ctx); ok {
			err = tx.QueryRow(ctx, `
				SELECT w.id, w.name, wm.role
				FROM workspaces w
				JOIN workspace_members wm ON wm.workspace_id = w.id
				WHERE w.id = $1 AND wm.user_id = $2 AND w.is_demo = FALSE
			`, requestedWS, userID).Scan(&workspaceID, &workspaceName, &role)
		} else {
			err = tx.QueryRow(ctx, `
				SELECT w.id, w.name, wm.role
				FROM workspaces w
				JOIN workspace_members wm ON wm.workspace_id = w.id
				WHERE wm.user_id = $1 AND w.is_demo = FALSE
				ORDER BY (wm.role = 'owner') DESC, w.created_at ASC
				LIMIT 1
			`, userID).Scan(&workspaceID, &workspaceName, &role)
		}

		if err == nil {
			var inboundReceiptAlias string
			err = tx.QueryRow(ctx, `
				SELECT inbound_receipt_alias FROM workspace_settings
				WHERE workspace_id = $1
			`, workspaceID).Scan(&inboundReceiptAlias)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("failed to fetch workspace settings: %w", err)
			}

			result = &ProvisionResult{
				WorkspaceID:        workspaceID,
				WorkspaceName:      workspaceName,
				Role:               role,
				IsDemo:             false,
				DemoModeEnabled:    false,
				InboundReceiptMail: inboundReceiptAlias,
				IsNewUser:          false,
			}
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to check existing workspace: %w", err)
		}

		// 2. User has no workspace: create personal workspace under RLS
		newWorkspaceID, receiptAlias, err := p.createOwnedWorkspaceTx(ctx, tx, userID, "Personal Workspace", "", "")
		if err != nil {
			return err
		}

		result = &ProvisionResult{
			WorkspaceID:        newWorkspaceID,
			WorkspaceName:      "Personal Workspace",
			Role:               "owner",
			IsDemo:             false,
			DemoModeEnabled:    false,
			InboundReceiptMail: receiptAlias,
			IsNewUser:          true,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CreatePersonalWorkspace creates the user's single owned non-demo workspace (e.g. when they
// first joined a household via invite and later want their own personal workspace).
// Returns ErrAlreadyOwnsWorkspace if the user already owns a non-demo workspace.
func (p *DefaultProvisioner) CreatePersonalWorkspace(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID uuid.UUID,
	name string,
	email string,
	displayName string,
) (*WorkspaceSummary, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" || len(trimmedName) > 255 {
		return nil, ErrInvalidWorkspaceName
	}

	var out *WorkspaceSummary
	err := db.WithWorkspaceTx(ctx, pool, userID, func(tx pgx.Tx) error {
		var alreadyOwns bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM workspaces w
				JOIN workspace_members wm ON wm.workspace_id = w.id
				WHERE wm.user_id = $1 AND wm.role = 'owner' AND w.is_demo = FALSE
			)
		`, userID).Scan(&alreadyOwns); err != nil {
			return fmt.Errorf("check existing owned workspace: %w", err)
		}
		if alreadyOwns {
			return ErrAlreadyOwnsWorkspace
		}

		wsID, _, err := p.createOwnedWorkspaceTx(ctx, tx, userID, trimmedName, email, displayName)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrAlreadyOwnsWorkspace
			}
			return err
		}
		out = &WorkspaceSummary{
			ID:     wsID,
			Name:   trimmedName,
			Role:   "owner",
			IsDemo: false,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (p *DefaultProvisioner) createOwnedWorkspaceTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	workspaceName string,
	email string,
	displayName string,
) (uuid.UUID, string, error) {
	newWorkspaceID := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo, owner_user_id)
		VALUES ($1, $2, FALSE, $3)
	`, newWorkspaceID, workspaceName, userID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("failed to create personal workspace: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, newWorkspaceID, userID); err != nil {
		return uuid.Nil, "", fmt.Errorf("failed to add workspace member: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_owners (workspace_id, owner_user_id)
		VALUES ($1, $2)
		ON CONFLICT (workspace_id) DO UPDATE SET owner_user_id = EXCLUDED.owner_user_id
	`, newWorkspaceID, userID); err != nil {
		return uuid.Nil, "", fmt.Errorf("failed to add workspace owner record: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_roster (workspace_id, user_id, role, email, display_name)
		VALUES ($1, $2, 'owner', $3, $4)
		ON CONFLICT (workspace_id, user_id) DO UPDATE
		SET role = 'owner',
		    email = COALESCE(NULLIF(EXCLUDED.email, ''), workspace_roster.email),
		    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), workspace_roster.display_name)
	`, newWorkspaceID, userID, strings.TrimSpace(email), strings.TrimSpace(displayName)); err != nil {
		return uuid.Nil, "", fmt.Errorf("failed to add workspace roster entry: %w", err)
	}

	rawUUID := strings.ReplaceAll(newWorkspaceID.String(), "-", "")
	aliasPrefix := rawUUID[:8]
	receiptAlias := fmt.Sprintf("w_%s@%s", aliasPrefix, p.ReceiptDomain)

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_settings (workspace_id, inbound_receipt_alias)
		VALUES ($1, $2)
	`, newWorkspaceID, receiptAlias); err != nil {
		return uuid.Nil, "", fmt.Errorf("failed to create workspace settings: %w", err)
	}

	if err = SeedDefaultCategories(ctx, tx, newWorkspaceID); err != nil {
		return uuid.Nil, "", fmt.Errorf("failed to seed default categories: %w", err)
	}

	return newWorkspaceID, receiptAlias, nil
}
