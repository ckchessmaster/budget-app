package transaction

import (
	"context"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type Service interface {
	CreateTransaction(ctx context.Context, tx pgx.Tx, req CreateSplitTransactionRequest) (*Transaction, error)
	GetTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error)
	ListTransactions(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, int, error)
	UpdateTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID, req UpdateTransactionRequest) (*Transaction, error)
	DeleteTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type service struct {
	repo        Repo
	accountRepo account.Repo
}

func NewService(repo Repo, accountRepo account.Repo) Service {
	return &service{
		repo:        repo,
		accountRepo: accountRepo,
	}
}

// ValidateSplits verifies that if splits are provided, their sum matches totalAmount to 2 decimal places.
func ValidateSplits(totalAmount decimal.Decimal, splits []SplitItem) error {
	if len(splits) == 0 {
		return nil
	}
	sum := decimal.Zero
	for _, s := range splits {
		sum = sum.Add(s.Amount.Round(2))
	}
	if !sum.Round(2).Equal(totalAmount.Round(2)) {
		return ErrSplitSumMismatch
	}
	return nil
}

func (s *service) CreateTransaction(ctx context.Context, tx pgx.Tx, req CreateSplitTransactionRequest) (*Transaction, error) {
	if req.AccountID == uuid.Nil {
		return nil, ErrMissingAccountID
	}
	if req.MerchantRaw == "" {
		return nil, ErrEmptyMerchant
	}
	if err := ValidateSplits(req.TotalAmount, req.Splits); err != nil {
		return nil, err
	}

	txDate := req.TransactionDate
	if txDate.IsZero() {
		txDate = time.Now().UTC()
	}

	origin := req.Origin
	if origin == "" {
		origin = OriginManualOffline
	}
	status := req.Status
	if status == "" {
		status = StatusPosted
	}
	isCleared := true
	if req.IsCleared != nil {
		isCleared = *req.IsCleared
	}

	var notesPtr *string
	if req.UserNotes != "" {
		n := req.UserNotes
		notesPtr = &n
	}

	parentCategoryID := req.CategoryID
	if len(req.Splits) > 0 {
		// Parent row of a split transaction must have category_id = NULL.
		parentCategoryID = nil
	}

	parent := &Transaction{
		AccountID:       req.AccountID,
		Origin:          origin,
		Status:          status,
		Amount:          req.TotalAmount.Round(2),
		MerchantRaw:     req.MerchantRaw,
		MerchantClean:   req.MerchantClean,
		CategoryID:      parentCategoryID,
		TransactionDate: txDate,
		UserNotes:       notesPtr,
		IsCleared:       isCleared,
	}

	if err := s.repo.Create(ctx, tx, parent); err != nil {
		return nil, err
	}

	if len(req.Splits) > 0 {
		parent.Splits = make([]Transaction, 0, len(req.Splits))
		for _, item := range req.Splits {
			var splitNotes *string
			if item.UserNotes != "" {
				n := item.UserNotes
				splitNotes = &n
			}
			child := Transaction{
				WorkspaceID:         parent.WorkspaceID,
				AccountID:           parent.AccountID,
				ParentTransactionID: &parent.ID,
				Origin:              parent.Origin,
				Status:              parent.Status,
				Amount:              item.Amount.Round(2),
				MerchantRaw:         parent.MerchantRaw,
				MerchantClean:       parent.MerchantClean,
				CategoryID:          item.CategoryID,
				TransactionDate:     parent.TransactionDate,
				UserNotes:           splitNotes,
				IsCleared:           parent.IsCleared,
			}
			if err := s.repo.Create(ctx, tx, &child); err != nil {
				return nil, err
			}
			parent.Splits = append(parent.Splits, child)
		}
	}

	if s.accountRepo != nil {
		if err := s.accountRepo.AdjustBalance(ctx, tx, parent.AccountID, parent.Amount); err != nil {
			return nil, err
		}
	}

	return parent, nil
}

func (s *service) GetTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error) {
	return s.repo.GetByID(ctx, tx, id)
}

func (s *service) ListTransactions(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, int, error) {
	return s.repo.List(ctx, tx, filter)
}

func (s *service) UpdateTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID, req UpdateTransactionRequest) (*Transaction, error) {
	existing, err := s.repo.GetByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	oldAmount := existing.Amount
	if req.CategoryID != nil {
		existing.CategoryID = req.CategoryID
	}
	if req.MerchantClean != nil {
		existing.MerchantClean = req.MerchantClean
	}
	if req.UserNotes != nil {
		existing.UserNotes = req.UserNotes
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}
	if req.IsCleared != nil {
		existing.IsCleared = *req.IsCleared
	}
	if req.Amount != nil {
		existing.Amount = req.Amount.Round(2)
	}

	if err := s.repo.Update(ctx, tx, existing); err != nil {
		return nil, err
	}

	delta := existing.Amount.Sub(oldAmount)
	if !delta.IsZero() && existing.ParentTransactionID == nil && s.accountRepo != nil {
		if err := s.accountRepo.AdjustBalance(ctx, tx, existing.AccountID, delta); err != nil {
			return nil, err
		}
	}

	return existing, nil
}

func (s *service) DeleteTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	existing, err := s.repo.GetByID(ctx, tx, id)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, tx, id); err != nil {
		return err
	}

	if existing.ParentTransactionID == nil && s.accountRepo != nil {
		if err := s.accountRepo.AdjustBalance(ctx, tx, existing.AccountID, existing.Amount.Neg()); err != nil {
			return err
		}
	}

	return nil
}
