package budget

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type BaselineService interface {
	InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error
	GetMonthlyBaselines(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error)
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
