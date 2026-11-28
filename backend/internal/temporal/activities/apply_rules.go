package activities

import (
	"context"

	"github.com/ckchessmaster/budget-app/backend/internal/categorization"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/jackc/pgx/v5"
)

// ApplyCategorizationRulesActivity runs priority-ordered workspace categorization rules
// against uncategorized, non-transfer, non-split parent transactions on budget accounts.
func (a *BankActivities) ApplyCategorizationRulesActivity(ctx context.Context, scope temporalinternal.SyncScope) (int, error) {
	ctx = db.WithWorkspaceID(ctx, scope.WorkspaceID)
	applied := 0
	err := db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		var rErr error
		applied, rErr = categorization.ApplyRulesToWorkspace(ctx, tx, scope.WorkspaceID)
		return rErr
	})
	return applied, err
}
