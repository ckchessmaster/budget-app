package account

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type Service interface {
	CreateAccount(ctx context.Context, tx pgx.Tx, a *Account) error
	GetAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error)
	ListAccounts(ctx context.Context, tx pgx.Tx) ([]Account, error)
	UpdateAccount(ctx context.Context, tx pgx.Tx, a *Account) error
	DeleteAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	AdjustBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, delta decimal.Decimal) error
}

type service struct {
	repo Repo
}

func NewService(repo Repo) Service {
	return &service{repo: repo}
}

func (s *service) CreateAccount(ctx context.Context, tx pgx.Tx, a *Account) error {
	return s.repo.Create(ctx, tx, a)
}

func (s *service) GetAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error) {
	return s.repo.GetByID(ctx, tx, id)
}

func (s *service) ListAccounts(ctx context.Context, tx pgx.Tx) ([]Account, error) {
	return s.repo.ListByWorkspace(ctx, tx)
}

func (s *service) UpdateAccount(ctx context.Context, tx pgx.Tx, a *Account) error {
	return s.repo.Update(ctx, tx, a)
}

func (s *service) DeleteAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return s.repo.Delete(ctx, tx, id)
}

func (s *service) AdjustBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, delta decimal.Decimal) error {
	return s.repo.AdjustBalance(ctx, tx, id, delta)
}
