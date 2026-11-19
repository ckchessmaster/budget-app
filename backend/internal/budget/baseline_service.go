package budget

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type BaselineService interface {
	InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error
	GetMonthlyBaselines(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error)
	SetMonthlyPlanned(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time, amount decimal.Decimal) error
}

type baselineService struct {
	repo BaselineRepo
}

func NewBaselineService(repo BaselineRepo) BaselineService {
	return &baselineService{repo: repo}
}

func (s *baselineService) InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error {
	return s.repo.InitializeMonth(ctx, tx, NormalizeYearMonth(yearMonth))
}

func (s *baselineService) GetMonthlyBaselines(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error) {
	return s.repo.GetByMonth(ctx, tx, NormalizeYearMonth(yearMonth))
}

func (s *baselineService) SetMonthlyPlanned(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time, amount decimal.Decimal) error {
	if amount.LessThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	return s.repo.UpsertMonthlyPlanned(ctx, tx, categoryID, NormalizeYearMonth(yearMonth), amount.Round(2))
}
