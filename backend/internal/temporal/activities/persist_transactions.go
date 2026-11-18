package activities

import (
	"context"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	"github.com/ckchessmaster/budget-app/backend/pkg/merchant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type PersistTransactionsInput struct {
	WorkspaceID uuid.UUID
	Data        *simplefin.DataResponse
}

// PersistTransactionsActivity upserts SimpleFIN transactions into the ledger,
// applying manual-to-bank deduplication and pending-to-posted reconciliation.
func (a *BankActivities) PersistTransactionsActivity(ctx context.Context, input PersistTransactionsInput) (int, error) {
	if input.Data == nil || len(input.Data.Transactions) == 0 {
		return 0, nil
	}

	normalizer := merchant.NewStandardNormalizer()
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Map external_account_id -> internal account_id
	acctMap := make(map[string]uuid.UUID)
	rows, err := tx.Query(ctx, `
		SELECT id, external_account_id
		FROM accounts
		WHERE workspace_id = $1 AND external_account_id IS NOT NULL
	`, input.WorkspaceID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id uuid.UUID
		var extID string
		if err := rows.Scan(&id, &extID); err != nil {
			rows.Close()
			return 0, err
		}
		acctMap[extID] = id
	}
	rows.Close()

	// Update account balances from SimpleFIN
	for _, extAcct := range input.Data.Accounts {
		internalID, ok := acctMap[extAcct.ID]
		if !ok {
			continue
		}
		if bal, err := decimal.NewFromString(extAcct.Balance); err == nil {
			_, _ = tx.Exec(ctx, `
				UPDATE accounts SET current_balance = $2, updated_at = NOW() WHERE id = $1
			`, internalID, bal.Round(2))
		}
	}

	manualPool, err := loadTxPool(ctx, tx, input.WorkspaceID, "origin = 'MANUAL_OFFLINE'")
	if err != nil {
		return 0, err
	}
	pendingPool, err := loadTxPool(ctx, tx, input.WorkspaceID, "status = 'PENDING' AND origin = 'BANK_FEED'")
	if err != nil {
		return 0, err
	}

	persisted := 0
	for _, dto := range input.Data.Transactions {
		acctID, ok := acctMap[dto.AccountID]
		if !ok {
			continue
		}

		// Skip if external_transaction_id already exists
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM transactions WHERE workspace_id = $1 AND external_transaction_id = $2)
		`, input.WorkspaceID, dto.ID).Scan(&exists); err != nil {
			return persisted, err
		}
		if exists {
			continue
		}

		amt, err := decimal.NewFromString(dto.Amount)
		if err != nil {
			continue
		}
		amt = amt.Round(2)

		txDate := time.Unix(dto.Posted, 0).UTC()
		status := "POSTED"
		isCleared := true
		if dto.Pending {
			status = "PENDING"
			isCleared = false
		}
		cleanName := normalizer.Clean(dto.Description)

		incoming := TxRecord{
			AccountID:       acctID,
			ExternalID:      dto.ID,
			Amount:          amt,
			MerchantRaw:     dto.Description,
			MerchantClean:   cleanName,
			TransactionDate: txDate,
			Status:          status,
		}

		// Conflict Resolution Strategy 1: promote matching MANUAL_OFFLINE entry
		if manualMatch := MatchIncomingBankToManual(incoming, manualPool); manualMatch.Matched {
			if _, err := tx.Exec(ctx, `
				UPDATE transactions
				SET external_transaction_id = $2,
				    origin = 'BANK_FEED',
				    status = $3,
				    is_cleared = $4,
				    merchant_clean = COALESCE(NULLIF(merchant_clean, ''), $5)
				WHERE id = $1
			`, manualMatch.ManualTxID, dto.ID, status, isCleared, cleanName); err != nil {
				return persisted, err
			}
			persisted++
			continue
		}

		// Pending -> Posted reconciliation
		if status == "POSTED" {
			if pendingMatch := MatchPostedToPending(incoming, pendingPool); pendingMatch.IsMatched {
				if _, err := tx.Exec(ctx, `
					UPDATE transactions
					SET external_transaction_id = $2,
					    status = 'POSTED',
					    is_cleared = TRUE,
					    amount = $3,
					    merchant_clean = COALESCE(NULLIF(merchant_clean, ''), $4)
					WHERE id = $1
				`, pendingMatch.ExistingID, dto.ID, amt, cleanName); err != nil {
					return persisted, err
				}
				persisted++
				continue
			}
		}

		// Insert new bank feed transaction
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (
				workspace_id, account_id, external_transaction_id, origin, status,
				amount, merchant_raw, merchant_clean, transaction_date, is_cleared
			)
			VALUES ($1, $2, $3, 'BANK_FEED', $4, $5, $6, $7, $8, $9)
		`, input.WorkspaceID, acctID, dto.ID, status, amt, dto.Description, cleanName, txDate.Format("2006-01-02"), isCleared); err != nil {
			return persisted, err
		}
		persisted++
	}

	if _, err := tx.Exec(ctx, `
		UPDATE simplefin_connections SET last_synced_at = NOW() WHERE workspace_id = $1
	`, input.WorkspaceID); err != nil {
		return persisted, err
	}

	if err := tx.Commit(ctx); err != nil {
		return persisted, err
	}
	return persisted, nil
}

func loadTxPool(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, cond string) ([]TxRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, account_id, COALESCE(external_transaction_id, ''), amount, merchant_raw,
		       COALESCE(merchant_clean, ''), transaction_date, status, origin
		FROM transactions
		WHERE workspace_id = $1
		  AND parent_transaction_id IS NULL
		  AND `+cond, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pool []TxRecord
	for rows.Next() {
		var r TxRecord
		if err := rows.Scan(
			&r.ID, &r.AccountID, &r.ExternalID, &r.Amount, &r.MerchantRaw,
			&r.MerchantClean, &r.TransactionDate, &r.Status, &r.Origin,
		); err != nil {
			return nil, err
		}
		pool = append(pool, r)
	}
	return pool, rows.Err()
}
