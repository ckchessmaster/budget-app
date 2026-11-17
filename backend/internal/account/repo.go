package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type AccountRepo interface {
	Create(ctx context.Context, tx pgx.Tx, a *Account) error
	GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error)
	ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Account, error)
	Update(ctx context.Context, tx pgx.Tx, a *Account) error
	UpdateBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, delta decimal.Decimal) error
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type pgAccountRepo struct{}

func NewAccountRepo() AccountRepo {
	return &pgAccountRepo{}
}

func resolveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var workspaceID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT workspace_id FROM workspace_members LIMIT 1`).Scan(&workspaceID); err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve workspace_id: %w", err)
	}
	return workspaceID, nil
}

func (r *pgAccountRepo) Create(ctx context.Context, tx pgx.Tx, a *Account) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.WorkspaceID == uuid.Nil {
		wsID, err := resolveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		a.WorkspaceID = wsID
	}
	if a.CardMode == "" {
		a.CardMode = PayInFull
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO accounts (
			id, workspace_id, external_account_id, name, type, current_balance,
			is_on_budget, card_mode, initial_debt_baseline, apr_percentage,
			statement_due_day, debt_payoff_category_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING is_credit_card, created_at, updated_at
	`,
		a.ID, a.WorkspaceID, a.ExternalAccountID, a.Name, string(a.Type), a.CurrentBalance,
		a.IsOnBudget, string(a.CardMode), a.InitialDebtBaseline, a.APRPercentage,
		a.StatementDueDay, a.DebtPayoffCategoryID,
	).Scan(&a.IsCreditCard, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create account: %w", err)
	}
	return nil
}

func (r *pgAccountRepo) GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error) {
	var a Account
	var acctType, cardMode string
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, external_account_id, name, type, current_balance,
		       is_on_budget, is_credit_card, card_mode, initial_debt_baseline,
		       apr_percentage, statement_due_day, debt_payoff_category_id,
		       created_at, updated_at
		FROM accounts
		WHERE id = $1
	`, id).Scan(
		&a.ID, &a.WorkspaceID, &a.ExternalAccountID, &a.Name, &acctType, &a.CurrentBalance,
		&a.IsOnBudget, &a.IsCreditCard, &cardMode, &a.InitialDebtBaseline,
		&a.APRPercentage, &a.StatementDueDay, &a.DebtPayoffCategoryID,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	a.Type = AccountType(acctType)
	a.CardMode = CCMode(cardMode)
	return &a, nil
}

func (r *pgAccountRepo) ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Account, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, external_account_id, name, type, current_balance,
		       is_on_budget, is_credit_card, card_mode, initial_debt_baseline,
		       apr_percentage, statement_due_day, debt_payoff_category_id,
		       created_at, updated_at
		FROM accounts
		ORDER BY type ASC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		var a Account
		var acctType, cardMode string
		if err := rows.Scan(
			&a.ID, &a.WorkspaceID, &a.ExternalAccountID, &a.Name, &acctType, &a.CurrentBalance,
			&a.IsOnBudget, &a.IsCreditCard, &cardMode, &a.InitialDebtBaseline,
			&a.APRPercentage, &a.StatementDueDay, &a.DebtPayoffCategoryID,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}
		a.Type = AccountType(acctType)
		a.CardMode = CCMode(cardMode)
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating accounts: %w", err)
	}
	return accounts, nil
}

func (r *pgAccountRepo) Update(ctx context.Context, tx pgx.Tx, a *Account) error {
	tag, err := tx.Exec(ctx, `
		UPDATE accounts
		SET name = $2,
		    type = $3,
		    current_balance = $4,
		    is_on_budget = $5,
		    card_mode = $6,
		    initial_debt_baseline = $7,
		    apr_percentage = $8,
		    statement_due_day = $9,
		    debt_payoff_category_id = $10,
		    updated_at = NOW()
		WHERE id = $1
	`,
		a.ID, a.Name, string(a.Type), a.CurrentBalance, a.IsOnBudget,
		string(a.CardMode), a.InitialDebtBaseline, a.APRPercentage,
		a.StatementDueDay, a.DebtPayoffCategoryID,
	)
	if err != nil {
		return fmt.Errorf("failed to update account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgAccountRepo) UpdateBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, delta decimal.Decimal) error {
	tag, err := tx.Exec(ctx, `
		UPDATE accounts
		SET current_balance = current_balance + $2,
		    updated_at = NOW()
		WHERE id = $1
	`, id, delta)
	if err != nil {
		return fmt.Errorf("failed to update account balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *pgAccountRepo) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
