package budget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type BaselineRepo interface {
	InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error
	GetByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error)
	GetByCategoryAndMonth(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time) (*MonthlyCategoryBudget, error)
	UpsertMonthlyPlanned(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time, originalPlanned decimal.Decimal) error
}

type pgBaselineRepo struct{}

func NewBaselineRepo() BaselineRepo {
	return &pgBaselineRepo{}
}

func (r *pgBaselineRepo) InitializeMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) error {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return err
	}
	ym := NormalizeYearMonth(yearMonth)
	prevYM := ym.AddDate(0, -1, 0)

	_, err = tx.Exec(ctx, `
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
		WHERE c.workspace_id = $3
		ON CONFLICT (workspace_id, category_id, year_month) DO NOTHING
	`, ym, prevYM, wsID)
	if err != nil {
		return fmt.Errorf("failed to initialize month baselines: %w", err)
	}
	return nil
}

func (r *pgBaselineRepo) GetByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyCategoryBudget, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	ym := NormalizeYearMonth(yearMonth)
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, category_id, year_month, original_planned, carryover_balance
		FROM monthly_category_budgets
		WHERE workspace_id = $1 AND year_month = $2::date
		ORDER BY category_id ASC
	`, wsID, ym)
	if err != nil {
		return nil, fmt.Errorf("failed to get monthly baselines: %w", err)
	}
	defer rows.Close()

	var result []MonthlyCategoryBudget
	for rows.Next() {
		var b MonthlyCategoryBudget
		if err := rows.Scan(&b.ID, &b.WorkspaceID, &b.CategoryID, &b.YearMonth, &b.OriginalPlanned, &b.CarryoverBalance); err != nil {
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
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	ym := NormalizeYearMonth(yearMonth)
	var b MonthlyCategoryBudget
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, category_id, year_month, original_planned, carryover_balance
		FROM monthly_category_budgets
		WHERE workspace_id = $1 AND category_id = $2 AND year_month = $3::date
	`, wsID, categoryID, ym).Scan(&b.ID, &b.WorkspaceID, &b.CategoryID, &b.YearMonth, &b.OriginalPlanned, &b.CarryoverBalance)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Fallback to category's default monthly_budget if baseline not yet frozen
			var catWSID uuid.UUID
			var defBudget decimal.Decimal
			err2 := tx.QueryRow(ctx, `SELECT workspace_id, monthly_budget FROM categories WHERE id = $1 AND workspace_id = $2`, categoryID, wsID).Scan(&catWSID, &defBudget)
			if err2 != nil {
				if err2 == pgx.ErrNoRows {
					return nil, ErrNotFound
				}
				return nil, fmt.Errorf("failed to get category fallback budget: %w", err2)
			}
			return &MonthlyCategoryBudget{
				WorkspaceID:      catWSID,
				CategoryID:       categoryID,
				YearMonth:        ym,
				OriginalPlanned:  defBudget,
				CarryoverBalance: decimal.Zero,
			}, nil
		}
		return nil, fmt.Errorf("failed to get monthly baseline: %w", err)
	}
	return &b, nil
}

func (r *pgBaselineRepo) UpsertMonthlyPlanned(ctx context.Context, tx pgx.Tx, categoryID uuid.UUID, yearMonth time.Time, originalPlanned decimal.Decimal) error {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return err
	}
	ym := NormalizeYearMonth(yearMonth)

	var catWSID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT workspace_id FROM categories WHERE id = $1 AND workspace_id = $2`, categoryID, wsID).Scan(&catWSID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("failed to resolve category workspace: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned)
		VALUES ($1, $2, $3::date, $4)
		ON CONFLICT (workspace_id, category_id, year_month)
		DO UPDATE SET original_planned = EXCLUDED.original_planned
	`, catWSID, categoryID, ym, originalPlanned.Round(2))
	if err != nil {
		return fmt.Errorf("failed to upsert monthly planned budget: %w", err)
	}
	return nil
}
