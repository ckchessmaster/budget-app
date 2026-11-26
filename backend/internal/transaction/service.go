package transaction

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

type Service interface {
	CreateTransaction(ctx context.Context, tx pgx.Tx, req CreateSplitTransactionRequest) (*Transaction, error)
	CreateTransactionIdempotent(ctx context.Context, tx pgx.Tx, req CreateSplitTransactionRequest) (*Transaction, bool, error)
	GetTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error)
	ListTransactions(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, int, error)
	UpdateTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID, req UpdateTransactionRequest) (*Transaction, error)
	IgnoreReviewBefore(ctx context.Context, tx pgx.Tx, beforeMonth time.Time) (int64, error)
	ReplaceSplits(ctx context.Context, tx pgx.Tx, id uuid.UUID, req ReplaceSplitsRequest) (*Transaction, error)
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

// CreateTransactionIdempotent creates a transaction guarded by req.ClientID, and reports
// whether this call is the one that wrote it.
//
// The boolean is the whole point: a replayed item must not repeat the side effects that
// follow an insert, above all the balance adjustment, so callers need to know whether they
// applied the row or found it already applied. A conflict is success, not an error.
//
// The pre-check is a SELECT rather than ON CONFLICT DO NOTHING because the insert and the
// balance adjustment share one transaction: a statement that swallows the conflict cannot
// also tell the caller to skip the adjustment.
//
// Two concurrent attempts with the same key can both pass the pre-check. One loses on the
// unique index, which aborts its transaction, and the caller retries — where the pre-check
// now finds the row. That is why the 23505 is returned untouched instead of being turned
// into a duplicate report: the row cannot be read from an aborted transaction.
func (s *service) CreateTransactionIdempotent(
	ctx context.Context,
	tx pgx.Tx,
	req CreateSplitTransactionRequest,
) (*Transaction, bool, error) {
	if req.ClientID == "" {
		created, err := s.CreateTransaction(ctx, tx, req)
		if err != nil {
			return nil, false, err
		}
		return created, true, nil
	}

	existing, err := s.repo.GetByClientID(ctx, tx, req.ClientID)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrTransactionNotFound) {
		return nil, false, err
	}

	created, err := s.CreateTransaction(ctx, tx, req)
	if err != nil {
		var pqErr *pgconn.PgError
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			// Lost the race. Retryable on purpose: the retry takes the pre-check.
			return nil, false, fmt.Errorf("%w (client_id %s already applied concurrently)", err, req.ClientID)
		}
		return nil, false, err
	}
	return created, true, nil
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
	parentPlannedIncomeID := req.PlannedIncomeID
	if len(req.Splits) > 0 {
		// Parent row of a split transaction must have category_id = NULL.
		parentCategoryID = nil
		parentPlannedIncomeID = nil
	}

	parent := &Transaction{
		AccountID:       req.AccountID,
		Origin:          origin,
		Status:          status,
		Amount:          req.TotalAmount.Round(2),
		MerchantRaw:     req.MerchantRaw,
		MerchantClean:   req.MerchantClean,
		CategoryID:      parentCategoryID,
		PlannedIncomeID: parentPlannedIncomeID,
		TransactionDate: txDate,
		UserNotes:       notesPtr,
		IsCleared:       isCleared,
	}
	if req.ClientID != "" {
		key := req.ClientID
		parent.ClientID = &key
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
			var splitCatID, splitIncID *uuid.UUID
			if item.PlannedIncomeID != nil && *item.PlannedIncomeID != uuid.Nil {
				splitIncID = item.PlannedIncomeID
			} else {
				splitCatID = item.CategoryID
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
				CategoryID:          splitCatID,
				PlannedIncomeID:     splitIncID,
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

	hasSplits := len(existing.Splits) > 0
	oldAmount := existing.Amount
	assignmentChanged := false
	if !hasSplits {
		if req.ClearCategory {
			existing.CategoryID = nil
			assignmentChanged = true
		} else if req.CategoryID != nil {
			existing.CategoryID = req.CategoryID
			existing.PlannedIncomeID = nil
			assignmentChanged = true
		}
		if req.ClearPlannedIncome {
			existing.PlannedIncomeID = nil
			assignmentChanged = true
		} else if req.PlannedIncomeID != nil {
			existing.PlannedIncomeID = req.PlannedIncomeID
			existing.CategoryID = nil
			assignmentChanged = true
		}
	}
	if assignmentChanged {
		existing.CategorizedBy = "MANUAL"
		existing.AppliedRuleID = nil
		existing.NeedsReview = false
	} else if req.ApproveReview {
		existing.NeedsReview = false
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
		nextAmt := req.Amount.Round(2)
		if hasSplits && !nextAmt.Equal(oldAmount) {
			return nil, ErrCannotSplitTransaction
		}
		existing.Amount = nextAmt
	}
	if req.ClearBudgetMonth {
		existing.BudgetMonth = nil
	} else if req.BudgetMonth != nil {
		raw := *req.BudgetMonth
		var parsed time.Time
		var parseErr error
		if len(raw) == 7 {
			parsed, parseErr = time.Parse("2006-01", raw)
		} else {
			parsed, parseErr = time.Parse("2006-01-02", raw)
		}
		if parseErr != nil {
			return nil, ErrInvalidBudgetMonth
		}
		normalized := time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC)
		existing.BudgetMonth = &normalized
	}
	if req.ReviewIgnored != nil {
		existing.ReviewIgnored = *req.ReviewIgnored
	}

	if err := s.repo.Update(ctx, tx, existing); err != nil {
		return nil, err
	}
	if hasSplits {
		if err := s.repo.PropagateParentFieldsToSplits(ctx, tx, existing); err != nil {
			return nil, err
		}
	}

	delta := existing.Amount.Sub(oldAmount)
	if !delta.IsZero() && existing.ParentTransactionID == nil && s.accountRepo != nil {
		if err := s.accountRepo.AdjustBalance(ctx, tx, existing.AccountID, delta); err != nil {
			return nil, err
		}
	}

	return existing, nil
}

func (s *service) IgnoreReviewBefore(ctx context.Context, tx pgx.Tx, beforeMonth time.Time) (int64, error) {
	return s.repo.IgnoreReviewBefore(ctx, tx, beforeMonth)
}

// ReplaceSplits replaces the child split rows of a transaction atomically, or unsplits it
// back to a single category/income assignment when req.Splits is empty.
func (s *service) ReplaceSplits(ctx context.Context, tx pgx.Tx, id uuid.UUID, req ReplaceSplitsRequest) (*Transaction, error) {
	parent, err := s.repo.GetByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if parent.ParentTransactionID != nil || parent.IsTransfer || parent.Amount.IsZero() {
		return nil, ErrCannotSplitTransaction
	}
	if s.accountRepo != nil {
		acct, acctErr := s.accountRepo.GetByID(ctx, tx, parent.AccountID)
		if acctErr != nil {
			return nil, acctErr
		}
		if !acct.IsOnBudget {
			return nil, ErrCannotSplitTransaction
		}
	}

	if len(req.Splits) == 0 {
		if err := s.repo.DeleteSplitsByParent(ctx, tx, parent.ID); err != nil {
			return nil, err
		}
		if req.PlannedIncomeID != nil && *req.PlannedIncomeID != uuid.Nil {
			parent.PlannedIncomeID = req.PlannedIncomeID
			parent.CategoryID = nil
		} else if req.CategoryID != nil && *req.CategoryID != uuid.Nil {
			parent.CategoryID = req.CategoryID
			parent.PlannedIncomeID = nil
		} else {
			parent.CategoryID = nil
			parent.PlannedIncomeID = nil
		}
		parent.CategorizedBy = "MANUAL"
		parent.AppliedRuleID = nil
		parent.NeedsReview = false
		if err := s.repo.Update(ctx, tx, parent); err != nil {
			return nil, err
		}
		parent.Splits = nil
		return parent, nil
	}

	if len(req.Splits) < 2 {
		return nil, ErrSplitTooFewItems
	}

	parentSign := parent.Amount.Sign()
	for _, item := range req.Splits {
		amt := item.Amount.Round(2)
		if amt.IsZero() || amt.Sign() != parentSign {
			return nil, ErrSplitSignMismatch
		}
		hasCat := item.CategoryID != nil && *item.CategoryID != uuid.Nil
		hasInc := item.PlannedIncomeID != nil && *item.PlannedIncomeID != uuid.Nil
		if hasCat == hasInc {
			return nil, ErrSplitCategoryRequired
		}
	}

	if err := ValidateSplits(parent.Amount, req.Splits); err != nil {
		return nil, err
	}

	if err := s.repo.DeleteSplitsByParent(ctx, tx, parent.ID); err != nil {
		return nil, err
	}

	parent.CategoryID = nil
	parent.PlannedIncomeID = nil
	parent.CategorizedBy = "MANUAL"
	parent.AppliedRuleID = nil
	parent.NeedsReview = false
	if err := s.repo.Update(ctx, tx, parent); err != nil {
		return nil, err
	}

	parent.Splits = make([]Transaction, 0, len(req.Splits))
	for _, item := range req.Splits {
		var splitNotes *string
		if item.UserNotes != "" {
			n := item.UserNotes
			splitNotes = &n
		}
		var catID, incID *uuid.UUID
		if item.PlannedIncomeID != nil && *item.PlannedIncomeID != uuid.Nil {
			incID = item.PlannedIncomeID
		} else {
			catID = item.CategoryID
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
			CategoryID:          catID,
			PlannedIncomeID:     incID,
			ReviewIgnored:       parent.ReviewIgnored,
			TransactionDate:     parent.TransactionDate,
			BudgetMonth:         parent.BudgetMonth,
			UserNotes:           splitNotes,
			IsCleared:           parent.IsCleared,
		}
		if err := s.repo.Create(ctx, tx, &child); err != nil {
			return nil, err
		}
		parent.Splits = append(parent.Splits, child)
	}

	return parent, nil
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
