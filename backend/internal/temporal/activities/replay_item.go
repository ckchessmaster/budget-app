package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/transaction"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"
)

// ReplayActivities applies one queued offline transaction at a time.
//
// This is the body of the loop that used to run inside POST /api/v1/transactions/sync. The
// loop is a workflow now, so each item gets the things a request handler could not give it:
// its own transaction boundary, its own retry budget, and a verdict that outlives the tab
// that queued it.
//
// One item per activity, not one per batch. A batch-sized activity would retry all hundred
// entries because one of them hit a deadlock, which is precisely the failure this task
// removes.
type ReplayActivities struct {
	Pool         *pgxpool.Pool
	Transactions transaction.Service
}

// ReplayItemInput is the scope plus the single item to apply. The scope travels with every
// item because row level security is keyed on the session GUC: an unscoped write matches
// zero rows and still looks like success.
type ReplayItemInput struct {
	temporalinternal.SyncScope
	Item temporalinternal.ReplayItem `json:"item"`
}

// ReplayItemResult confirms the row this item produced. Duplicate reports an item that was
// already applied by an earlier attempt, which is a success with no new row behind it.
type ReplayItemResult struct {
	ClientID      string    `json:"client_id"`
	TransactionID uuid.UUID `json:"transaction_id"`
	Duplicate     bool      `json:"duplicate,omitempty"`
}

// terminalReplayError builds the rejection shape: non-retryable, with a machine-readable
// type the workflow forwards as the reason so the browser can say "fix this" instead of
// resending forever.
func terminalReplayError(reason, message string, cause error) error {
	return temporal.NewNonRetryableApplicationError(message, reason, cause)
}

// ClassifyReplayError decides whether a failure is worth another attempt.
//
// The rule is what the user can still fix. A bad date, an unknown account, or a merchant
// name the schema will not accept will fail identically forever, so they are terminal and
// the queue surfaces them. A deadlock, a serialization conflict, a statement timeout, or a
// dropped connection is a momentary condition, so it retries with the workflow's backoff.
//
// A unique violation on client_id is deliberately left retryable rather than reported as a
// duplicate: it means another attempt won the race, and the retry's pre-check turns it into
// a clean duplicate once this transaction has rolled back.
func ClassifyReplayError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, transaction.ErrEmptyMerchant):
		return terminalReplayError(transaction.ReasonEmptyMerchant, "merchant name is required", err)
	case errors.Is(err, transaction.ErrMissingAccountID):
		return terminalReplayError(transaction.ReasonMissingAccountID, "an account is required", err)
	case errors.Is(err, transaction.ErrSplitSumMismatch):
		return terminalReplayError("split_sum_mismatch", "split amounts must equal the total", err)
	}

	var pqErr *pgconn.PgError
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23503", // foreign_key_violation
			"23514", // check_violation
			"22001", // string_data_right_truncation
			"22003", // numeric_value_out_of_range
			"22P02": // invalid_text_representation
			return terminalReplayError("invalid_transaction_value",
				fmt.Sprintf("the stored value was rejected: %s", pqErr.Message), err)
		}
	}

	return err
}

// parseReplayDate accepts the two shapes the offline queue and the API both emit.
//
// An unparseable date is a rejection, not a fallback. The synchronous handler used to
// substitute time.Now() here, which silently moved the transaction into whatever month the
// retry happened to land in and changed the budget it was deducted from.
func parseReplayDate(raw string) (time.Time, error) {
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("unparseable transaction date")
}

// ReplayTransactionItemActivity applies one queued item and confirms the row.
//
// It is idempotent by client_id, so an at-least-once delivery creates one transaction. The
// account and category are checked inside the same scoped transaction as the insert: the
// foreign key alone is not enough, because Postgres does not apply row level security to
// foreign key lookups, so a guessed id from another workspace would otherwise be accepted.
func (a *ReplayActivities) ReplayTransactionItemActivity(
	ctx context.Context,
	input ReplayItemInput,
) (ReplayItemResult, error) {
	if input.WorkspaceID == uuid.Nil || input.UserID == uuid.Nil {
		return ReplayItemResult{}, terminalReplayError(
			"invalid_replay_scope", "workspace_id and user_id are required to replay a transaction", nil)
	}
	if a.Transactions == nil {
		return ReplayItemResult{}, fmt.Errorf("transaction service is not configured for replay")
	}

	item := input.Item
	if item.ClientID == "" {
		return ReplayItemResult{}, terminalReplayError(
			"missing_client_id", "every queued transaction needs an idempotency key", nil)
	}
	if item.MerchantRaw == "" {
		return ReplayItemResult{}, terminalReplayError(
			transaction.ReasonEmptyMerchant, "merchant name is required", transaction.ErrEmptyMerchant)
	}
	if item.AccountID == uuid.Nil {
		return ReplayItemResult{}, terminalReplayError(
			transaction.ReasonMissingAccountID, "an account is required", transaction.ErrMissingAccountID)
	}

	txDate, err := parseReplayDate(item.TransactionDate)
	if err != nil {
		return ReplayItemResult{}, terminalReplayError(
			transaction.ReasonInvalidTransactionDate,
			fmt.Sprintf("%q is not a transaction date", item.TransactionDate), err)
	}

	var result ReplayItemResult
	ctx = db.WithWorkspaceID(ctx, input.WorkspaceID)
	err = db.WithWorkspaceTx(ctx, a.Pool, input.UserID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM accounts
				WHERE id = $1 AND workspace_id = $2
			)
		`, item.AccountID, input.WorkspaceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return terminalReplayError(transaction.ReasonUnknownAccount,
				"the queued account no longer exists in this workspace", nil)
		}

		if item.CategoryID != nil {
			var catExists bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM categories
					WHERE id = $1 AND workspace_id = $2
				)
			`, *item.CategoryID, input.WorkspaceID).Scan(&catExists); err != nil {
				return err
			}
			if !catExists {
				return terminalReplayError(transaction.ReasonUnknownCategory,
					"the queued category no longer exists in this workspace", nil)
			}
		}

		isCleared := false
		created, applied, err := a.Transactions.CreateTransactionIdempotent(ctx, tx, transaction.CreateSplitTransactionRequest{
			AccountID:       item.AccountID,
			TotalAmount:     item.Amount,
			MerchantRaw:     item.MerchantRaw,
			CategoryID:      item.CategoryID,
			Origin:          transaction.OriginManualOffline,
			Status:          transaction.StatusPending,
			TransactionDate: txDate,
			UserNotes:       item.UserNotes,
			IsCleared:       &isCleared,
			ClientID:        item.ClientID,
		})
		if err != nil {
			return err
		}

		result = ReplayItemResult{
			ClientID:      item.ClientID,
			TransactionID: created.ID,
			Duplicate:     !applied,
		}
		return nil
	})
	if err != nil {
		return ReplayItemResult{}, ClassifyReplayError(err)
	}

	return result, nil
}
