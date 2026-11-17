package budget

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type ReallocationService interface {
	TransferFunds(ctx context.Context, tx pgx.Tx, req ReallocationRequest) error
	GetEffectiveTarget(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (decimal.Decimal, error)
}

type reallocationService struct {
	catRepo      CategoryRepo
	baselineRepo BaselineRepo
	reallocRepo  ReallocationRepo
}

func NewReallocationService(catRepo CategoryRepo, baselineRepo BaselineRepo, reallocRepo ReallocationRepo) ReallocationService {
	return &reallocationService{
		catRepo:      catRepo,
		baselineRepo: baselineRepo,
		reallocRepo:  reallocRepo,
	}
}

func (s *reallocationService) TransferFunds(ctx context.Context, tx pgx.Tx, req ReallocationRequest) error {
	if !req.Amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	if req.FromCategoryID == req.ToCategoryID {
		return ErrSameCategoryTransfer
	}

	req.YearMonth = NormalizeYearMonth(req.YearMonth)

	fromCat, err := s.catRepo.GetByID(ctx, tx, req.FromCategoryID)
	if err != nil {
		return err
	}
	if _, err := s.catRepo.GetByID(ctx, tx, req.ToCategoryID); err != nil {
		return err
	}

	if err := s.baselineRepo.InitializeMonth(ctx, tx, req.YearMonth); err != nil {
		return err
	}

	return s.reallocRepo.Create(ctx, tx, fromCat.WorkspaceID, req)
}

func (s *reallocationService) GetEffectiveTarget(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (decimal.Decimal, error) {
	ym := NormalizeYearMonth(yearMonth)
	baseline, err := s.baselineRepo.GetByCategoryAndMonth(ctx, tx, categoryID, ym)
	if err != nil {
		return decimal.Zero, err
	}
	netTransfer, err := s.reallocRepo.GetNetTransferForCategory(ctx, tx, categoryID, ym)
	if err != nil {
		return decimal.Zero, err
	}
	return baseline.OriginalPlanned.Add(netTransfer), nil
}
