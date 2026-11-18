package activities

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TransferPair struct {
	OutflowID uuid.UUID
	InflowID  uuid.UUID
}

// FindTransferPairs identifies pairs of unlinked transactions across different accounts
// with exact offsetting amounts (Amount_A = -Amount_B) within +-3 days.
func FindTransferPairs(txs []TxRecord) []TransferPair {
	used := make(map[uuid.UUID]bool)
	var pairs []TransferPair

	for i := 0; i < len(txs); i++ {
		a := txs[i]
		if used[a.ID] || a.Amount.IsZero() {
			continue
		}
		for j := i + 1; j < len(txs); j++ {
			b := txs[j]
			if used[b.ID] || b.AccountID == a.AccountID {
				continue
			}
			if !a.Amount.Add(b.Amount).IsZero() {
				continue
			}
			daysDiff := math.Abs(a.TransactionDate.Sub(b.TransactionDate).Hours() / 24.0)
			if daysDiff > 3.0 {
				continue
			}

			used[a.ID] = true
			used[b.ID] = true
			if a.Amount.IsNegative() {
				pairs = append(pairs, TransferPair{OutflowID: a.ID, InflowID: b.ID})
			} else {
				pairs = append(pairs, TransferPair{OutflowID: b.ID, InflowID: a.ID})
			}
			break
		}
	}
	return pairs
}

// DetectAndLinkTransfersActivity scans recent unlinked transactions for a workspace
// and links offsetting cross-account transfers with category_id = NULL.
func (a *BankActivities) DetectAndLinkTransfersActivity(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, account_id, amount, merchant_raw, transaction_date
		FROM transactions
		WHERE workspace_id = $1
		  AND parent_transaction_id IS NULL
		  AND transfer_linked_transaction_id IS NULL
		  AND transaction_date >= CURRENT_DATE - INTERVAL '30 days'
		ORDER BY transaction_date ASC
	`, workspaceID)
	if err != nil {
		return 0, err
	}

	var records []TxRecord
	for rows.Next() {
		var r TxRecord
		if err := rows.Scan(&r.ID, &r.AccountID, &r.Amount, &r.MerchantRaw, &r.TransactionDate); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	pairs := FindTransferPairs(records)
	for _, p := range pairs {
		if err := linkTransferPair(ctx, tx, p.OutflowID, p.InflowID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(pairs), nil
}

func linkTransferPair(ctx context.Context, tx pgx.Tx, aID, bID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET transfer_linked_transaction_id = $1, category_id = NULL WHERE id = $2
	`, bID, aID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transactions SET transfer_linked_transaction_id = $1, category_id = NULL WHERE id = $2
	`, aID, bID); err != nil {
		return err
	}
	return nil
}
