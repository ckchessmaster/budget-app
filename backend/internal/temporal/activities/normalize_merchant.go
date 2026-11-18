package activities

import (
	"context"

	"github.com/ckchessmaster/budget-app/backend/pkg/merchant"
	"github.com/google/uuid"
)

// NormalizeMerchantsActivity cleans raw merchant strings into merchant_clean for a workspace.
func (a *BankActivities) NormalizeMerchantsActivity(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	normalizer := merchant.NewStandardNormalizer()

	rows, err := a.Pool.Query(ctx, `
		SELECT id, merchant_raw
		FROM transactions
		WHERE workspace_id = $1
		  AND (merchant_clean IS NULL OR merchant_clean = '')
	`, workspaceID)
	if err != nil {
		return 0, err
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
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	updated := 0
	for _, it := range items {
		clean := normalizer.Clean(it.raw)
		if clean == "" {
			continue
		}
		if _, err := a.Pool.Exec(ctx, `
			UPDATE transactions SET merchant_clean = $2 WHERE id = $1
		`, it.id, clean); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
