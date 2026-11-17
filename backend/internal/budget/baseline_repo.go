package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type BaselineRepo interface {
	InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error
	GetByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error)
	GetByCategoryAndMonth(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (*MonthlyCategoryBudget, error)
}

type pgBaselineRepo struct{}

func NewBaselineRepo() BaselineRepo {
	return &pgBaselineRepo{}
}

func (r *pgBaselineRepo) InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error {
	ym := NormalizeYearMonth(yearMonth)
	prevYM := ym.AddDate(0, -1, 0)

	_, err := tx.Exec(ctx, `
		INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned)
		SELECT c.workspace_id,
		       c.id,
		       $1::date,
		       COALESCE(prev.original_planned, c.monthly_budget)
		FROM categories c
		LEFT JOIN monthly_category_budgets prev
		       ON prev.workspace_id = c.workspace_id
		      AND prev.category_id = c.id
		      AND prev.year_month = $2::date
		ON CONFLICT (workspace_id, category_id, year_month) DO NOTHING
	`, ym, prevYM)
	if err != nil {
		return fmt.Errorf("failed to initialize month baselines: %w", err)
	}
	return nil
}

func (r *pgBaselineRepo) GetByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error) {
	ym := NormalizeYearMonth(yearMonth)
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, category_id, year_month, original_planned
		FROM monthly_category_budgets
		WHERE year_month = $1::date
		ORDER BY category_id ASC
	`, ym)
	if err != nil {
		return nil, fmt.Errorf("failed to get monthly baselines: %w", err)
	}
	defer rows.Close()

	var result []MonthlyCategoryBudget
	for rows.Next() {
		var b MonthlyCategoryBudget
		if err := rows.Scan(&b.ID, &b.WorkspaceID, &b.CategoryID, &b.YearMonth, &b.OriginalPlanned); err != nil {
			return nil, fmt.Errorf("failed to scan monthly baseline: %w", err)
		}
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating monthly baselines: %w", err)
	}
	return result, nil
}

func (r *pgBaselineRepo) GetByCategoryAndMonth(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (*MonthlyCategoryBudget, error) {
	ym := NormalizeYearMonth(yearMonth)
	var b MonthlyCategoryBudget
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, category_id, year_month, original_planned
		FROM monthly_category_budgets
		WHERE category_id = $1 AND year_month = $2::date
	`, categoryID, ym).Scan(&b.ID, &b.WorkspaceID, &b.CategoryID, &b.YearMonth, &b.OriginalPlanned)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Fallback to category's default monthly_budget if baseline not yet frozen
			var wsID uuid.UUID
			var defBudget decimal.Decimal
			err2 := tx.QueryRow(ctx, `SELECT workspace_id, monthly_budget FROM categories WHERE id = $1`, categoryID).Scan(&wsID, &defBudget)
			if err2 != nil {
				if err2 == pgx.ErrNoRows {
					return nil, ErrNotFound
				}
				return nil, fmt.Errorf("failed to get category fallback budget: %w", err2)
			}
			return &MonthlyCategoryBudget{
				WorkspaceID:     wsID,
				CategoryID:      categoryID,
				YearMonth:       ym,
				OriginalPlanned: defBudget,
			}, nil
		}
		return nil, fmt.Errorf("failed to get monthly baseline: %w", err)
	}
	return &b, nil
}
