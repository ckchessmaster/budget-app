package transaction

import (
	"errors"
	"strings"

	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type PaginatedTransactionsResponse struct {
	Items []Transaction `json:"items"`
	Total int           `json:"total"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
}

type OfflineSyncItem struct {
	// ClientID is the idempotency key the browser generated when the item was queued.
	ClientID string `json:"client_id"`

	// LegacyLocalID is the pre-TASK-5.2.4 spelling of the same key. A cached bundle still
	// sends it, and a key that bundle cannot send is a key that duplicates on retry, so it
	// is accepted as a fallback rather than dropped.
	LegacyLocalID   string          `json:"local_id"`
	AccountID       uuid.UUID       `json:"account_id"`
	Amount          decimal.Decimal `json:"amount"`
	MerchantRaw     string          `json:"merchant_raw"`
	CategoryID      *uuid.UUID      `json:"category_id"`
	TransactionDate string          `json:"transaction_date"`
	UserNotes       string          `json:"user_notes,omitempty"`
}

// maxClientIDLength matches transactions.client_id. Anything longer is a malformed client,
// not a key, and would fail at the insert instead of at the edge.
const maxClientIDLength = 128

// ErrClientKeyRequired reports a queued item with no idempotency key.
//
// It is a request-level rejection rather than a per-item one: without a key there is no way
// to report the outcome back to the right queue entry, so nothing in the batch is safe to
// apply.
var ErrClientKeyRequired = errors.New("client_id_required")

// replayItem converts a submitted entry into the workflow payload, resolving the idempotency
// key from either spelling.
func (i OfflineSyncItem) replayItem() (temporalinternal.ReplayItem, error) {
	key := strings.TrimSpace(i.ClientID)
	if key == "" {
		key = strings.TrimSpace(i.LegacyLocalID)
	}
	if key == "" {
		return temporalinternal.ReplayItem{}, ErrClientKeyRequired
	}
	if len(key) > maxClientIDLength {
		return temporalinternal.ReplayItem{}, ErrClientKeyRequired
	}

	return temporalinternal.ReplayItem{
		ClientID:        key,
		AccountID:       i.AccountID,
		Amount:          i.Amount,
		MerchantRaw:     i.MerchantRaw,
		CategoryID:      i.CategoryID,
		TransactionDate: i.TransactionDate,
		UserNotes:       i.UserNotes,
	}, nil
}

type SyncTransactionsRequest struct {
	Items []OfflineSyncItem `json:"items"`
}

// SyncAcceptedResponse acknowledges that a batch is queued in Temporal.
//
// The per-item verdicts are no longer here: they live in the workflow result, which the
// client reads from GET /api/v1/transactions/workflow-status/{workflow_id} and which stays
// available after the tab that submitted the batch is gone.
type SyncAcceptedResponse struct {
	WorkflowID string `json:"workflow_id"`
	Status     string `json:"status"`
	ItemCount  int    `json:"item_count"`
}
