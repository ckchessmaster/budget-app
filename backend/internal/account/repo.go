package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type AccountType string

const (
	Checking   AccountType = "checking"
	Savings    AccountType = "savings"
	Cash       AccountType = "cash"
	CreditCard AccountType = "credit_card"
	Investment AccountType = "investment"
	Loan       AccountType = "loan"
)

type CCMode string

const (
	PayInFull   CCMode = "PAY_IN_FULL"
	DebtPaydown CCMode = "DEBT_PAYDOWN"
)

var (
	ErrAccountNotFound          = errors.New("account not found")
	ErrInvalidAccountType       = errors.New("invalid account type")
	ErrInvalidCardMode          = errors.New("invalid credit card mode")
	ErrDebtPaydownNonCreditCard = errors.New("only credit_card accounts can use DEBT_PAYDOWN mode")
	ErrMissingDebtPayoffCat     = errors.New("debt_payoff_category_id is required for DEBT_PAYDOWN mode")
	ErrInvalidStatementDueDay   = errors.New("statement_due_day must be between 1 and 31")
	ErrEmptyAccountName         = errors.New("account name is required")
)

type Account struct {
	ID                   uuid.UUID       `json:"id"`
	WorkspaceID          uuid.UUID       `json:"workspace_id"`
	ExternalAccountID    *string         `json:"external_account_id"`
	Name                 string          `json:"name"`
	Type                 AccountType     `json:"type"`
	CurrentBalance       decimal.Decimal `json:"current_balance"`
	IsOnBudget           bool            `json:"is_on_budget"`
	CardMode             CCMode          `json:"card_mode"`
	InitialDebtBaseline  decimal.Decimal `json:"initial_debt_baseline"`
	APRPercentage        decimal.Decimal `json:"apr_percentage"`
	StatementDueDay      *int            `json:"statement_due_day"`
	DebtPayoffCategoryID *uuid.UUID      `json:"debt_payoff_category_id"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type Repo interface {
	Create(ctx context.Context, tx pgx.Tx, a *Account) error
	GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error)
	ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Account, error)
	Update(ctx context.Context, tx pgx.Tx, a *Account) error
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
	AdjustBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, delta decimal.Decimal) error
}

type pgRepo struct{}

func NewRepo() Repo {
	return &pgRepo{}
}

func resolveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var workspaceID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT workspace_id FROM workspace_members LIMIT 1`).Scan(&workspaceID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve workspace_id: %w", err)
	}
	return workspaceID, nil
}

// ValidateAccount enforces domain invariants for accounts and credit card modes.
func ValidateAccount(a *Account) error {
	if a.Name == "" {
		return ErrEmptyAccountName
	}

	switch a.Type {
	case Checking, Savings, Cash, CreditCard, Investment, Loan:
	default:
		return ErrInvalidAccountType
	}

	if a.CardMode == "" {
		a.CardMode = PayInFull
	}

	switch a.CardMode {
	case PayInFull, DebtPaydown:
	default:
		return ErrInvalidCardMode
	}

	if a.CardMode == DebtPaydown {
		if a.Type != CreditCard {
			return ErrDebtPaydownNonCreditCard
		}
		if a.DebtPayoffCategoryID == nil || *a.DebtPayoffCategoryID == uuid.Nil {
			return ErrMissingDebtPayoffCat
		}
	}

	if a.StatementDueDay != nil && (*a.StatementDueDay < 1 || *a.StatementDueDay > 31) {
		return ErrInvalidStatementDueDay
	}

	return nil
}

func (r *pgRepo) Create(ctx context.Context, tx pgx.Tx, a *Account) error {
	if err := ValidateAccount(a); err != nil {
		return err
	}
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

	return tx.QueryRow(ctx, `
		INSERT INTO accounts (
			id, workspace_id, external_account_id, name, type, current_balance,
			is_on_budget, card_mode, initial_debt_baseline, apr_percentage,
			statement_due_day, debt_payoff_category_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at, updated_at
	`,
		a.ID, a.WorkspaceID, a.ExternalAccountID, a.Name, string(a.Type),
		a.CurrentBalance.Round(2), a.IsOnBudget, string(a.CardMode),
		a.InitialDebtBaseline.Round(2), a.APRPercentage.Round(2),
		a.StatementDueDay, a.DebtPayoffCategoryID,
	).Scan(&a.CreatedAt, &a.UpdatedAt)
}

func (r *pgRepo) GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Account, error) {
	var a Account
	var accType, cardMode string
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, external_account_id, name, type, current_balance,
		       is_on_budget, card_mode, initial_debt_baseline, apr_percentage,
		       statement_due_day, debt_payoff_category_id, created_at, updated_at
		FROM accounts
		WHERE id = $1
	`, id).Scan(
		&a.ID, &a.WorkspaceID, &a.ExternalAccountID, &a.Name, &accType, &a.CurrentBalance,
		&a.IsOnBudget, &cardMode, &a.InitialDebtBaseline, &a.APRPercentage,
		&a.StatementDueDay, &a.DebtPayoffCategoryID, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	a.Type = AccountType(accType)
	a.CardMode = CCMode(cardMode)
	return &a, nil
}

func (r *pgRepo) ListByWorkspace(ctx context.Context, tx pgx.Tx) ([]Account, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, external_account_id, name, type, current_balance,
		       is_on_budget, card_mode, initial_debt_baseline, apr_percentage,
		       statement_due_day, debt_payoff_category_id, created_at, updated_at
		FROM accounts
		ORDER BY type ASC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var accType, cardMode string
		if err := rows.Scan(
			&a.ID, &a.WorkspaceID, &a.ExternalAccountID, &a.Name, &accType, &a.CurrentBalance,
			&a.IsOnBudget, &cardMode, &a.InitialDebtBaseline, &a.APRPercentage,
			&a.StatementDueDay, &a.DebtPayoffCategoryID, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.Type = AccountType(accType)
		a.CardMode = CCMode(cardMode)
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (r *pgRepo) Update(ctx context.Context, tx pgx.Tx, a *Account) error {
	if err := ValidateAccount(a); err != nil {
		return err
	}
	err := tx.QueryRow(ctx, `
		UPDATE accounts
		SET external_account_id = $2,
		    name = $3,
		    type = $4,
		    current_balance = $5,
		    is_on_budget = $6,
		    card_mode = $7,
		    initial_debt_baseline = $8,
		    apr_percentage = $9,
		    statement_due_day = $10,
		    debt_payoff_category_id = $11,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`,
		a.ID, a.ExternalAccountID, a.Name, string(a.Type), a.CurrentBalance.Round(2),
		a.IsOnBudget, string(a.CardMode), a.InitialDebtBaseline.Round(2), a.APRPercentage.Round(2),
		a.StatementDueDay, a.DebtPayoffCategoryID,
	).Scan(&a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		return err
	}
	return nil
}

func (r *pgRepo) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (r *pgRepo) AdjustBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, delta decimal.Decimal) error {
	tag, err := tx.Exec(ctx, `
		UPDATE accounts
		SET current_balance = current_balance + $2,
		    updated_at = NOW()
		WHERE id = $1
	`, id, delta.Round(2))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAccountNotFound
	}
	return nil
}
