package activities

import (
	"context"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/ckchessmaster/budget-app/backend/pkg/merchant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NormalizeMerchantsActivity cleans raw merchant strings into merchant_clean for a workspace.
func (a *BankActivities) NormalizeMerchantsActivity(ctx context.Context, scope temporalinternal.SyncScope) (int, error) {
	ctx = db.WithWorkspaceID(ctx, scope.WorkspaceID)
	normalizer := merchant.NewStandardNormalizer()

	updated := 0
	// RLS-scoped: see FetchSimpleFINDataActivity. The read and the UPDATEs have to share
	// one scoped transaction; on an unscoped connection the SELECT returns nothing and
	// the updates would match zero rows without ever reporting an error.
	err := db.WithWorkspaceTx(ctx, a.Pool, scope.UserID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, merchant_raw
			FROM transactions
			WHERE workspace_id = $1
			  AND (merchant_clean IS NULL OR merchant_clean = '')
		`, scope.WorkspaceID)
		if err != nil {
			return err
		}

		type rawItem struct {
			id  uuid.UUID
			raw string
		}
		var items []rawItem
		for rows.Next() {
			var it rawItem
			if err := rows.Scan(&it.id, &it.raw); err != nil {
				rows.Close()
				return err
			}
			items = append(items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		updated = 0
		for _, it := range items {
			clean := normalizer.Clean(it.raw)
			if clean == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				UPDATE transactions SET merchant_clean = $2 WHERE id = $1
			`, it.id, clean); err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	return updated, err
}
