package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
)

type ProvisionResult struct {
	WorkspaceID        uuid.UUID `json:"workspace_id"`
	WorkspaceName      string    `json:"workspace_name"`
	IsDemo             bool      `json:"is_demo"`
	DemoModeEnabled    bool      `json:"demo_mode_enabled"`
	InboundReceiptMail string    `json:"inbound_receipt_mail"`
	IsNewUser          bool      `json:"is_new_user"`
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
			err = tx.QueryRow(ctx, `
				SELECT w.id, w.name
				FROM workspaces w
				JOIN workspace_members wm ON wm.workspace_id = w.id
				WHERE wm.user_id = $1 AND w.is_demo = TRUE
				ORDER BY w.created_at ASC
				LIMIT 1
			`, userID).Scan(&workspaceID, &workspaceName)
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

		// 1. Check if user already has a personal (non-demo) workspace
		var workspaceID uuid.UUID
		var workspaceName string
		err = tx.QueryRow(ctx, `
			SELECT w.id, w.name
			FROM workspaces w
			JOIN workspace_members wm ON wm.workspace_id = w.id
			WHERE wm.user_id = $1 AND w.is_demo = FALSE
			ORDER BY w.created_at ASC
			LIMIT 1
		`, userID).Scan(&workspaceID, &workspaceName)

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
		newWorkspaceID := uuid.New()
		workspaceName = "Personal Workspace"
		_, err = tx.Exec(ctx, `
			INSERT INTO workspaces (id, name, is_demo)
			VALUES ($1, $2, FALSE)
		`, newWorkspaceID, workspaceName)
		if err != nil {
			return fmt.Errorf("failed to create personal workspace: %w", err)
		}

		// Add member as owner
		_, err = tx.Exec(ctx, `
			INSERT INTO workspace_members (workspace_id, user_id, role)
			VALUES ($1, $2, 'owner')
		`, newWorkspaceID, userID)
		if err != nil {
			return fmt.Errorf("failed to add workspace member: %w", err)
		}

		// Generate inbound receipt alias: w_<first_8_chars>@receipts.yourdomain.com
		rawUUID := strings.ReplaceAll(newWorkspaceID.String(), "-", "")
		aliasPrefix := rawUUID[:8]
		receiptAlias := fmt.Sprintf("w_%s@%s", aliasPrefix, p.ReceiptDomain)

		// Insert workspace settings
		_, err = tx.Exec(ctx, `
			INSERT INTO workspace_settings (workspace_id, inbound_receipt_alias)
			VALUES ($1, $2)
		`, newWorkspaceID, receiptAlias)
		if err != nil {
			return fmt.Errorf("failed to create workspace settings: %w", err)
		}

		// Seed default categories
		if err = SeedDefaultCategories(ctx, tx, newWorkspaceID); err != nil {
			return fmt.Errorf("failed to seed default categories: %w", err)
		}

		result = &ProvisionResult{
			WorkspaceID:        newWorkspaceID,
			WorkspaceName:      workspaceName,
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
