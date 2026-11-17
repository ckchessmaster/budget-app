package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type ReallocationRepo interface {
	Create(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, req ReallocationRequest) error
	GetNetTransferForCategory(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (decimal.Decimal, error)
}

type pgReallocationRepo struct{}

func NewReallocationRepo() ReallocationRepo {
	return &pgReallocationRepo{}
}

func (r *pgReallocationRepo) Create(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, req ReallocationRequest) error {
	ym := NormalizeYearMonth(req.YearMonth)
	if workspaceID == uuid.Nil {
		wsID, err := resolveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		workspaceID = wsID
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO budget_reallocations (workspace_id, year_month, from_category_id, to_category_id, amount, reason)
		VALUES ($1, $2::date, $3, $4, $5, $6)
	`, workspaceID, ym, req.FromCategoryID, req.ToCategoryID, req.Amount, req.Reason)
	if err != nil {
		return fmt.Errorf("failed to create budget reallocation: %w", err)
	}
	return nil
}

func (r *pgReallocationRepo) GetNetTransferForCategory(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (decimal.Decimal, error) {
	ym := NormalizeYearMonth(yearMonth)
	var netTransfer decimal.Decimal
	err := tx.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN to_category_id = $1 THEN amount ELSE 0 END), 0) -
			COALESCE(SUM(CASE WHEN from_category_id = $1 THEN amount ELSE 0 END), 0)
		FROM budget_reallocations
		WHERE year_month = $2::date
		  AND (to_category_id = $1 OR from_category_id = $1)
	`, categoryID, ym).Scan(&netTransfer)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to compute net reallocation: %w", err)
	}
	return netTransfer, nil
}
