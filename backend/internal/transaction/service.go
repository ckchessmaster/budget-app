package transaction

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type SplitItem struct {
	Amount     decimal.Decimal `json:"amount"`
	CategoryID *uuid.UUID      `json:"category_id"`
	UserNotes  string          `json:"user_notes"`
}

type CreateSplitTransactionRequest struct {
	AccountID       uuid.UUID       `json:"account_id"`
	TotalAmount     decimal.Decimal `json:"total_amount"`
	MerchantRaw     string          `json:"merchant_raw"`
	TransactionDate time.Time       `json:"transaction_date"`
	Splits          []SplitItem     `json:"splits"`
}

type TransactionService interface {
	CreateSingle(ctx context.Context, tx pgx.Tx, t *Transaction) error
	CreateSplit(ctx context.Context, tx pgx.Tx, req CreateSplitTransactionRequest) (*Transaction, []Transaction, error)
}

type transactionService struct {
	repo TransactionRepo
}

func NewTransactionService(repo TransactionRepo) TransactionService {
	return &transactionService{repo: repo}
}

func ValidateSplits(totalAmount decimal.Decimal, splits []SplitItem) error {
	if len(splits) == 0 {
		return ErrEmptySplits
	}
	sum := decimal.Zero
	for _, s := range splits {
		sum = sum.Add(s.Amount)
	}
	if !sum.Round(2).Equal(totalAmount.Round(2)) {
		return ErrSplitSumMismatch
	}
	return nil
}

func (s *transactionService) CreateSingle(ctx context.Context, tx pgx.Tx, t *Transaction) error {
	return s.repo.Create(ctx, tx, t)
}

func (s *transactionService) CreateSplit(ctx context.Context, tx pgx.Tx, req CreateSplitTransactionRequest) (*Transaction, []Transaction, error) {
	if err := ValidateSplits(req.TotalAmount, req.Splits); err != nil {
		return nil, nil, err
	}

	parent := &Transaction{
		AccountID:       req.AccountID,
		Origin:          OriginManualOffline,
		Status:          StatusPosted,
		Amount:          req.TotalAmount.Round(2),
		MerchantRaw:     req.MerchantRaw,
		CategoryID:      nil, // Parent split row has category_id = NULL
		TransactionDate: req.TransactionDate,
		IsCleared:       true,
	}
	if err := s.repo.Create(ctx, tx, parent); err != nil {
		return nil, nil, err
	}

	children := make([]Transaction, 0, len(req.Splits))
	for _, item := range req.Splits {
		var notes *string
		if item.UserNotes != "" {
			n := item.UserNotes
			notes = &n
		}
		child := Transaction{
			WorkspaceID:         parent.WorkspaceID,
			AccountID:           req.AccountID,
			ParentTransactionID: &parent.ID,
			Origin:              OriginManualOffline,
			Status:              StatusPosted,
			Amount:              item.Amount.Round(2),
			MerchantRaw:         req.MerchantRaw,
			CategoryID:          item.CategoryID,
			TransactionDate:     req.TransactionDate,
			UserNotes:           notes,
			IsCleared:           true,
		}
		if err := s.repo.Create(ctx, tx, &child); err != nil {
			return nil, nil, err
		}
		children = append(children, child)
	}

	return parent, children, nil
}
