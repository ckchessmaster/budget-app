package transaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

var (
	ErrNotFound         = errors.New("transaction not found")
	ErrSplitSumMismatch = errors.New("sum of splits must equal total_amount to 2 decimal places")
	ErrEmptySplits      = errors.New("splits cannot be empty")
)

type TransactionOrigin string

const (
	OriginBankFeed      TransactionOrigin = "BANK_FEED"
	OriginManualOffline TransactionOrigin = "MANUAL_OFFLINE"
	OriginRecurring     TransactionOrigin = "RECURRING"
)

type TransactionStatus string

const (
	StatusPending TransactionStatus = "PENDING"
	StatusPosted  TransactionStatus = "POSTED"
)

type Transaction struct {
	ID                          uuid.UUID         `json:"id"`
	WorkspaceID                 uuid.UUID         `json:"workspace_id"`
	AccountID                   uuid.UUID         `json:"account_id"`
	ParentTransactionID         *uuid.UUID        `json:"parent_transaction_id"`
	ExternalTransactionID       *string           `json:"external_transaction_id"`
	Origin                      TransactionOrigin `json:"origin"`
	Status                      TransactionStatus `json:"status"`
	Amount                      decimal.Decimal   `json:"amount"`
	MerchantRaw                 string            `json:"merchant_raw"`
	MerchantClean               *string           `json:"merchant_clean"`
	CategoryID                  *uuid.UUID        `json:"category_id"`
	TransferLinkedTransactionID *uuid.UUID        `json:"transfer_linked_transaction_id"`
	IsTransfer                  bool              `json:"is_transfer"`
	TransactionDate             time.Time         `json:"transaction_date"`
	UserNotes                   *string           `json:"user_notes"`
	IsCleared                   bool              `json:"is_cleared"`
	CreatedAt                   time.Time         `json:"created_at"`
}

type TransactionFilter struct {
	AccountID  *uuid.UUID
	CategoryID *uuid.UUID
	Status     *string
	Page       int
	Limit      int
}

type TransactionRepo interface {
	Create(ctx context.Context, tx pgx.Tx, t *Transaction) error
	GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error)
	List(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, error)
	Update(ctx context.Context, tx pgx.Tx, id uuid.UUID, categoryID *uuid.UUID, userNotes *string, status *TransactionStatus, isCleared *bool) (*Transaction, error)
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type pgTransactionRepo struct{}

func NewTransactionRepo() TransactionRepo {
	return &pgTransactionRepo{}
}

func resolveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	var wsID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT workspace_id FROM workspace_members LIMIT 1`).Scan(&wsID); err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve workspace_id: %w", err)
	}
	return wsID, nil
}

func (r *pgTransactionRepo) Create(ctx context.Context, tx pgx.Tx, t *Transaction) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.WorkspaceID == uuid.Nil {
		wsID, err := resolveWorkspaceID(ctx, tx)
		if err != nil {
			return err
		}
		t.WorkspaceID = wsID
	}
	if t.Origin == "" {
		t.Origin = OriginManualOffline
	}
	if t.Status == "" {
		t.Status = StatusPosted
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO transactions (
			id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
			origin, status, amount, merchant_raw, merchant_clean, category_id,
			transfer_linked_transaction_id, transaction_date, user_notes, is_cleared
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::date, $14, $15)
		RETURNING is_transfer, created_at
	`,
		t.ID, t.WorkspaceID, t.AccountID, t.ParentTransactionID, t.ExternalTransactionID,
		string(t.Origin), string(t.Status), t.Amount, t.MerchantRaw, t.MerchantClean, t.CategoryID,
		t.TransferLinkedTransactionID, t.TransactionDate.UTC().Format("2006-01-02"), t.UserNotes, t.IsCleared,
	).Scan(&t.IsTransfer, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}
	return nil
}

func (r *pgTransactionRepo) GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error) {
	var t Transaction
	var origin, status string
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
		       origin, status, amount, merchant_raw, merchant_clean, category_id,
		       transfer_linked_transaction_id, is_transfer, transaction_date, user_notes,
		       is_cleared, created_at
		FROM transactions
		WHERE id = $1
	`, id).Scan(
		&t.ID, &t.WorkspaceID, &t.AccountID, &t.ParentTransactionID, &t.ExternalTransactionID,
		&origin, &status, &t.Amount, &t.MerchantRaw, &t.MerchantClean, &t.CategoryID,
		&t.TransferLinkedTransactionID, &t.IsTransfer, &t.TransactionDate, &t.UserNotes,
		&t.IsCleared, &t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}
	t.Origin = TransactionOrigin(origin)
	t.Status = TransactionStatus(status)
	return &t, nil
}

func (r *pgTransactionRepo) List(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	offset := (filter.Page - 1) * filter.Limit

	var where []string
	var args []any
	argIdx := 1

	if filter.AccountID != nil {
		where = append(where, fmt.Sprintf("account_id = $%d", argIdx))
		args = append(args, *filter.AccountID)
		argIdx++
	}
	if filter.CategoryID != nil {
		where = append(where, fmt.Sprintf("category_id = $%d", argIdx))
		args = append(args, *filter.CategoryID)
		argIdx++
	}
	if filter.Status != nil && *filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *filter.Status)
		argIdx++
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
		       origin, status, amount, merchant_raw, merchant_clean, category_id,
		       transfer_linked_transaction_id, is_transfer, transaction_date, user_notes,
		       is_cleared, created_at
		FROM transactions
		%s
		ORDER BY transaction_date DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)
	args = append(args, filter.Limit, offset)

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list transactions: %w", err)
	}
	defer rows.Close()

	txs := make([]Transaction, 0)
	for rows.Next() {
		var t Transaction
		var origin, status string
		if err := rows.Scan(
			&t.ID, &t.WorkspaceID, &t.AccountID, &t.ParentTransactionID, &t.ExternalTransactionID,
			&origin, &status, &t.Amount, &t.MerchantRaw, &t.MerchantClean, &t.CategoryID,
			&t.TransferLinkedTransactionID, &t.IsTransfer, &t.TransactionDate, &t.UserNotes,
			&t.IsCleared, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}
		t.Origin = TransactionOrigin(origin)
		t.Status = TransactionStatus(status)
		txs = append(txs, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transactions: %w", err)
	}
	return txs, nil
}

func (r *pgTransactionRepo) Update(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	categoryID *uuid.UUID,
	userNotes *string,
	status *TransactionStatus,
	isCleared *bool,
) (*Transaction, error) {
	var statusStr *string
	if status != nil {
		s := string(*status)
		statusStr = &s
	}

	tag, err := tx.Exec(ctx, `
		UPDATE transactions
		SET category_id = COALESCE($2, category_id),
		    user_notes = COALESCE($3, user_notes),
		    status = COALESCE($4, status),
		    is_cleared = COALESCE($5, is_cleared)
		WHERE id = $1
	`, id, categoryID, userNotes, statusStr, isCleared)
	if err != nil {
		return nil, fmt.Errorf("failed to update transaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetByID(ctx, tx, id)
}

func (r *pgTransactionRepo) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete transaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
