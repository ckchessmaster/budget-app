package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type PlannedIncomeRepo interface {
	Create(ctx context.Context, tx pgx.Tx, inc *MonthlyPlannedIncome) error
	ListByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyPlannedIncome, error)
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type pgPlannedIncomeRepo struct{}

func NewPlannedIncomeRepo() PlannedIncomeRepo {
	return &pgPlannedIncomeRepo{}
}

func (r *pgPlannedIncomeRepo) Create(ctx context.Context, tx pgx.Tx, inc *MonthlyPlannedIncome) error {
	if !inc.Amount.GreaterThan(decimal.Zero) {
		return ErrInvalidAmount
	}
	inc.YearMonth = NormalizeYearMonth(inc.YearMonth)
	if inc.ID == uuid.Nil {
		inc.ID = uuid.New()
	}
	if inc.WorkspaceID == uuid.Nil {
		wsID, err := resolveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		inc.WorkspaceID = wsID
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO monthly_planned_incomes (id, workspace_id, year_month, name, amount)
		VALUES ($1, $2, $3::date, $4, $5)
		RETURNING created_at
	`, inc.ID, inc.WorkspaceID, inc.YearMonth, inc.Name, inc.Amount).Scan(&inc.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create planned income: %w", err)
	}
	return nil
}

func (r *pgPlannedIncomeRepo) ListByMonth(ctx context.Context, tx pgx.Tx, yearMonth time.Time) ([]MonthlyPlannedIncome, error) {
	ym := NormalizeYearMonth(yearMonth)
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, year_month, name, amount, created_at
		FROM monthly_planned_incomes
		WHERE year_month = $1::date
		ORDER BY created_at ASC, name ASC
	`, ym)
	if err != nil {
		return nil, fmt.Errorf("failed to list planned incomes: %w", err)
	}
	defer rows.Close()

	incomes := make([]MonthlyPlannedIncome, 0)
	for rows.Next() {
		var inc MonthlyPlannedIncome
		if err := rows.Scan(&inc.ID, &inc.WorkspaceID, &inc.YearMonth, &inc.Name, &inc.Amount, &inc.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan planned income: %w", err)
		}
		incomes = append(incomes, inc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating planned incomes: %w", err)
	}
	return incomes, nil
}

func (r *pgPlannedIncomeRepo) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM monthly_planned_incomes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete planned income: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
