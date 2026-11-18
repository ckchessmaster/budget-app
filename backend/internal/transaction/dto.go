package transaction

import (
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
	LocalID         string          `json:"local_id"`
	AccountID       uuid.UUID       `json:"account_id"`
	Amount          decimal.Decimal `json:"amount"`
	MerchantRaw     string          `json:"merchant_raw"`
	CategoryID      *uuid.UUID      `json:"category_id"`
	TransactionDate string          `json:"transaction_date"`
	UserNotes       string          `json:"user_notes,omitempty"`
}

type SyncTransactionsRequest struct {
	Items []OfflineSyncItem `json:"items"`
}

type SyncItemResult struct {
	LocalID       string     `json:"local_id"`
	Status        string     `json:"status"` // "synced" | "failed"
	TransactionID *uuid.UUID `json:"transaction_id,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type SyncTransactionsResponse struct {
	Results []SyncItemResult `json:"results"`
}
