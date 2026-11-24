package activities

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/simplefin"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/ckchessmaster/budget-app/backend/pkg/merchant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type PersistTransactionsInput struct {
	temporalinternal.SyncScope

	Data *simplefin.DataResponse
}

type PersistTransactionsResult struct {
	FetchedCount       int      `json:"fetched_count"`
	InsertedCount      int      `json:"inserted_count"`
	SettledCount       int      `json:"settled_count"`
	ManualMatchedCount int      `json:"manual_matched_count"`
	Warnings           []string `json:"warnings,omitempty"`
}

func removePoolItem(pool []TxRecord, id uuid.UUID) []TxRecord {
	for i := range pool {
		if pool[i].ID == id {
			return append(pool[:i], pool[i+1:]...)
		}
	}
	return pool
}

// PersistTransactionsActivity upserts SimpleFIN transactions into the ledger,
// applying manual-to-bank deduplication and pending-to-posted reconciliation.
func (a *BankActivities) PersistTransactionsActivity(ctx context.Context, input PersistTransactionsInput) (PersistTransactionsResult, error) {
	if input.Data == nil {
		return PersistTransactionsResult{}, nil
	}

	result := PersistTransactionsResult{
		FetchedCount: len(input.Data.Transactions),
		Warnings:     input.Data.Errors,
	}

	normalizer := merchant.NewStandardNormalizer()
	// RLS-scoped: see FetchSimpleFINDataActivity. A plain pool transaction sees no tenant
	// rows, and every INSERT/UPDATE below would be rejected by the WITH CHECK clauses.
	tx, err := db.BeginWorkspaceTx(ctx, a.Pool, input.UserID)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Map external_account_id -> internal account_id
	acctMap := make(map[string]uuid.UUID)
	rows, err := tx.Query(ctx, `
		SELECT id, external_account_id
		FROM accounts
		WHERE workspace_id = $1 AND external_account_id IS NOT NULL
	`, input.SyncScope.WorkspaceID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id uuid.UUID
		var extID string
		if err := rows.Scan(&id, &extID); err != nil {
			rows.Close()
			return result, err
		}
		acctMap[extID] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}

	// Update account balances from SimpleFIN
	for _, extAcct := range input.Data.Accounts {
		internalID, ok := acctMap[extAcct.ID]
		if !ok {
			continue
		}
		if bal, err := decimal.NewFromString(extAcct.Balance); err == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE accounts SET current_balance = $2, updated_at = NOW() WHERE id = $1 AND workspace_id = $3
			`, internalID, bal.Round(2), input.SyncScope.WorkspaceID); err != nil {
				return result, err
			}
		}
	}

	manualPool, err := loadTxPool(ctx, tx, input.SyncScope.WorkspaceID, "origin = 'MANUAL_OFFLINE'")
	if err != nil {
		return result, err
	}
	pendingPool, err := loadTxPool(ctx, tx, input.SyncScope.WorkspaceID, "status = 'PENDING' AND origin = 'BANK_FEED'")
	if err != nil {
		return result, err
	}

	for _, dto := range input.Data.Transactions {
		acctID, ok := acctMap[dto.AccountID]
		if !ok {
			continue
		}

		// Skip if external_transaction_id already exists
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM transactions WHERE workspace_id = $1 AND external_transaction_id = $2)
		`, input.SyncScope.WorkspaceID, dto.ID).Scan(&exists); err != nil {
			return result, err
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
				    merchant_clean = COALESCE(NULLIF(merchant_clean, ''), $5),
				    amount = $6
				WHERE id = $1
			`, manualMatch.ManualTxID, dto.ID, status, isCleared, cleanName, amt); err != nil {
				return result, err
			}
			manualPool = removePoolItem(manualPool, manualMatch.ManualTxID)
			result.ManualMatchedCount++
			continue
		}

		// Pending -> Posted reconciliation
		if status == "POSTED" {
			if pendingMatch := MatchPostedToPending(incoming, pendingPool); pendingMatch.IsMatched {
				var settledDelta *decimal.Decimal
				if !pendingMatch.TipDelta.IsZero() {
					d := pendingMatch.TipDelta.Round(2)
					settledDelta = &d
				}
				if _, err := tx.Exec(ctx, `
					UPDATE transactions
					SET external_transaction_id = $2,
					    status = 'POSTED',
					    is_cleared = TRUE,
					    amount = $3,
					    merchant_clean = COALESCE(NULLIF(merchant_clean, ''), $4),
					    settled_delta = $5
					WHERE id = $1
				`, pendingMatch.ExistingID, dto.ID, amt, cleanName, settledDelta); err != nil {
					return result, err
				}
				pendingPool = removePoolItem(pendingPool, pendingMatch.ExistingID)
				result.SettledCount++
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
		`, input.SyncScope.WorkspaceID, acctID, dto.ID, status, amt, dto.Description, cleanName, txDate.Format("2006-01-02"), isCleared); err != nil {
			return result, err
		}
		result.InsertedCount++
	}

	warningsSlice := input.Data.Errors
	if warningsSlice == nil {
		warningsSlice = []string{}
	}
	warningsJSON, err := json.Marshal(warningsSlice)
	if err != nil {
		warningsJSON = []byte("[]")
	}

	if _, err := tx.Exec(ctx, `
		UPDATE simplefin_connections
		SET last_synced_at = NOW(),
		    last_sync_warnings = $2::jsonb
		WHERE workspace_id = $1
	`, input.SyncScope.WorkspaceID, string(warningsJSON)); err != nil {
		return result, err
	}

	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return result, nil
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
