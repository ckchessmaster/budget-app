package account

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AccountService interface {
	CreateAccount(ctx context.Context, tx pgx.Tx, a *Account) error
	GetAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error)
	ListAccounts(ctx context.Context, tx pgx.Tx) ([]Account, error)
	UpdateAccount(ctx context.Context, tx pgx.Tx, a *Account) error
	DeleteAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type accountService struct {
	repo AccountRepo
}

func NewAccountService(repo AccountRepo) AccountService {
	return &accountService{repo: repo}
}

func ValidateAccount(a *Account) error {
	switch a.Type {
	case Checking, Savings, CreditCard, Investment, Loan:
	default:
		return ErrInvalidAccountType
	}

	if a.CardMode == "" {
		a.CardMode = PayInFull
	}

	if a.CardMode == DebtPaydown {
		if a.Type != CreditCard {
			return ErrInvalidCardMode
		}
		if a.DebtPayoffCategoryID == nil || *a.DebtPayoffCategoryID == uuid.Nil {
			return ErrMissingDebtCategory
		}
	}

	return nil
}

func (s *accountService) CreateAccount(ctx context.Context, tx pgx.Tx, a *Account) error {
	if err := ValidateAccount(a); err != nil {
		return err
	}
	return s.repo.Create(ctx, tx, a)
}

func (s *accountService) GetAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error) {
	return s.repo.GetByID(ctx, tx, id)
}

func (s *accountService) ListAccounts(ctx context.Context, tx pgx.Tx) ([]Account, error) {
	return s.repo.ListByWorkspace(ctx, tx)
}

func (s *accountService) UpdateAccount(ctx context.Context, tx pgx.Tx, a *Account) error {
	if err := ValidateAccount(a); err != nil {
		return err
	}
	return s.repo.Update(ctx, tx, a)
}

func (s *accountService) DeleteAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return s.repo.Delete(ctx, tx, id)
}
