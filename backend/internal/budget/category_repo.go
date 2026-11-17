package budget

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CategoryRepo interface {
	Create(ctx context.Context, tx pgx.Tx, c *Category) error
	GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Category, error)
	ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Category, error)
	Update(ctx context.Context, tx pgx.Tx, c *Category) error
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type pgCategoryRepo struct{}

func NewCategoryRepo() CategoryRepo {
	return &pgCategoryRepo{}
}

func resolveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var workspaceID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT workspace_id FROM workspace_members LIMIT 1`).Scan(&workspaceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve workspace_id: %w", err)
	}
	return workspaceID, nil
}

func sanitizeCategory(c *Category) error {
	if c.ID != uuid.Nil && c.SweepTargetCategoryID != nil && *c.SweepTargetCategoryID == c.ID {
		return ErrCyclicSweep
	}
	if c.RolloverMode != RolloverSweep {
		c.SweepTargetCategoryID = nil
	}
	if c.TargetCadence == "" {
		c.TargetCadence = "NONE"
	}
	return nil
}

func (r *pgCategoryRepo) Create(ctx context.Context, tx pgx.Tx, c *Category) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if err := sanitizeCategory(c); err != nil {
		return err
	}
	if c.WorkspaceID == uuid.Nil {
		wsID, err := resolveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		c.WorkspaceID = wsID
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO categories (
			id, workspace_id, name, group_name, monthly_budget,
			rollover_mode, sweep_target_category_id, target_amount,
			target_date, target_cadence, sort_order
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`,
		c.ID, c.WorkspaceID, c.Name, c.GroupName, c.MonthlyBudget,
		string(c.RolloverMode), c.SweepTargetCategoryID, c.TargetAmount,
		c.TargetDate, c.TargetCadence, c.SortOrder,
	).Scan(&c.ID)
	if err != nil {
		return fmt.Errorf("failed to create category: %w", err)
	}
	return nil
}

func (r *pgCategoryRepo) GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Category, error) {
	var c Category
	var rolloverMode string
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, name, group_name, monthly_budget,
		       rollover_mode, sweep_target_category_id, target_amount,
		       target_date, target_cadence, sort_order
		FROM categories
		WHERE id = $1
	`, id).Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.GroupName, &c.MonthlyBudget,
		&rolloverMode, &c.SweepTargetCategoryID, &c.TargetAmount,
		&c.TargetDate, &c.TargetCadence, &c.SortOrder,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get category: %w", err)
	}
	c.RolloverMode = RolloverMode(rolloverMode)
	return &c, nil
}

func (r *pgCategoryRepo) ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Category, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, name, group_name, monthly_budget,
		       rollover_mode, sweep_target_category_id, target_amount,
		       target_date, target_cadence, sort_order
		FROM categories
		ORDER BY sort_order ASC, group_name ASC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list categories: %w", err)
	}
	defer rows.Close()

	categories := make([]Category, 0)
	for rows.Next() {
		var c Category
		var rolloverMode string
		if err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.Name, &c.GroupName, &c.MonthlyBudget,
			&rolloverMode, &c.SweepTargetCategoryID, &c.TargetAmount,
			&c.TargetDate, &c.TargetCadence, &c.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		c.RolloverMode = RolloverMode(rolloverMode)
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating categories: %w", err)
	}
	return categories, nil
}

func (r *pgCategoryRepo) Update(ctx context.Context, tx pgx.Tx, c *Category) error {
	if err := sanitizeCategory(c); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE categories
		SET name = $2,
		    group_name = $3,
		    monthly_budget = $4,
		    rollover_mode = $5,
		    sweep_target_category_id = $6,
		    target_amount = $7,
		    target_date = $8,
		    target_cadence = $9,
		    sort_order = $10
		WHERE id = $1
	`,
		c.ID, c.Name, c.GroupName, c.MonthlyBudget,
		string(c.RolloverMode), c.SweepTargetCategoryID, c.TargetAmount,
		c.TargetDate, c.TargetCadence, c.SortOrder,
	)
	if err != nil {
		return fmt.Errorf("failed to update category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgCategoryRepo) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM categories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
