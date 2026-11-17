package transaction

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CreateTransactionRequest struct {
	AccountID       uuid.UUID       `json:"account_id"`
	Amount          decimal.Decimal `json:"amount"`
	MerchantRaw     string          `json:"merchant_raw"`
	CategoryID      *uuid.UUID      `json:"category_id"`
	TransactionDate string          `json:"transaction_date"`
	UserNotes       *string         `json:"user_notes"`
	Origin          string          `json:"origin"`
	Status          string          `json:"status"`
	IsCleared       *bool           `json:"is_cleared"`
	Splits          []SplitItem     `json:"splits,omitempty"`
}

type UpdateTransactionRequest struct {
	CategoryID *uuid.UUID `json:"category_id"`
	UserNotes  *string    `json:"user_notes"`
	Status     *string    `json:"status"`
	IsCleared  *bool      `json:"is_cleared"`
}

type SyncTransactionItem struct {
	LocalID         string          `json:"local_id"`
	AccountID       uuid.UUID       `json:"account_id"`
	Amount          decimal.Decimal `json:"amount"`
	MerchantRaw     string          `json:"merchant_raw"`
	CategoryID      *uuid.UUID      `json:"category_id"`
	TransactionDate string          `json:"transaction_date"`
}

type SyncTransactionsRequest struct {
	Transactions []SyncTransactionItem `json:"transactions"`
}

type SyncResultItem struct {
	LocalID string `json:"local_id"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}
