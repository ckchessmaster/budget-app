package transaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type Origin string

const (
	OriginBankFeed      Origin = "BANK_FEED"
	OriginManualOffline Origin = "MANUAL_OFFLINE"
	OriginRecurring     Origin = "RECURRING"
)

type Status string

const (
	StatusPending Status = "PENDING"
	StatusPosted  Status = "POSTED"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrSplitSumMismatch    = errors.New("split amounts must equal total_amount")
	ErrEmptyMerchant       = errors.New("merchant_raw is required")
	ErrMissingAccountID    = errors.New("account_id is required")
)

type Transaction struct {
	ID                          uuid.UUID        `json:"id"`
	WorkspaceID                 uuid.UUID        `json:"workspace_id"`
	AccountID                   uuid.UUID        `json:"account_id"`
	ParentTransactionID         *uuid.UUID       `json:"parent_transaction_id"`
	ExternalTransactionID       *string          `json:"external_transaction_id"`
	Origin                      Origin           `json:"origin"`
	Status                      Status           `json:"status"`
	Amount                      decimal.Decimal  `json:"amount"`
	MerchantRaw                 string           `json:"merchant_raw"`
	MerchantClean               *string          `json:"merchant_clean"`
	CategoryID                  *uuid.UUID       `json:"category_id"`
	TransferLinkedTransactionID *uuid.UUID       `json:"transfer_linked_transaction_id"`
	IsTransfer                  bool             `json:"is_transfer"`
	SettledDelta                *decimal.Decimal `json:"settled_delta,omitempty"`
	TransactionDate             time.Time        `json:"transaction_date"`
	UserNotes                   *string          `json:"user_notes"`
	IsCleared                   bool             `json:"is_cleared"`
	CreatedAt                   time.Time        `json:"created_at"`
	Splits                      []Transaction    `json:"splits,omitempty"`
}

type SplitItem struct {
	Amount     decimal.Decimal `json:"amount"`
	CategoryID *uuid.UUID      `json:"category_id"`
	UserNotes  string          `json:"user_notes"`
}

type CreateSplitTransactionRequest struct {
	AccountID       uuid.UUID       `json:"account_id"`
	TotalAmount     decimal.Decimal `json:"total_amount"`
	MerchantRaw     string          `json:"merchant_raw"`
	MerchantClean   *string         `json:"merchant_clean,omitempty"`
	CategoryID      *uuid.UUID      `json:"category_id,omitempty"`
	Origin          Origin          `json:"origin,omitempty"`
	Status          Status          `json:"status,omitempty"`
	TransactionDate time.Time       `json:"transaction_date"`
	UserNotes       string          `json:"user_notes,omitempty"`
	IsCleared       *bool           `json:"is_cleared,omitempty"`
	Splits          []SplitItem     `json:"splits,omitempty"`
}

type UpdateTransactionRequest struct {
	CategoryID    *uuid.UUID       `json:"category_id"`
	ClearCategory bool             `json:"clear_category"`
	MerchantClean *string          `json:"merchant_clean"`
	UserNotes     *string          `json:"user_notes"`
	Status        *Status          `json:"status"`
	IsCleared     *bool            `json:"is_cleared"`
	Amount        *decimal.Decimal `json:"amount"`
}

type TransactionFilter struct {
	AccountID  *uuid.UUID `form:"account_id"`
	CategoryID *uuid.UUID `form:"category_id"`
	Status     *string    `form:"status"`
	Month      *time.Time `form:"month"`
	Page       int        `form:"page,default=1"`
	Limit      int        `form:"limit,default=50"`
}

type Repo interface {
	Create(ctx context.Context, tx pgx.Tx, t *Transaction) error
	GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error)
	List(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, int, error)
	Update(ctx context.Context, tx pgx.Tx, t *Transaction) error
	Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error
}

type pgRepo struct{}

func NewRepo() Repo {
	return &pgRepo{}
}

func resolveWorkspaceID(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	return db.ActiveWorkspaceID(ctx, tx)
}

func (r *pgRepo) Create(ctx context.Context, tx pgx.Tx, t *Transaction) error {
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

	return tx.QueryRow(ctx, `
		INSERT INTO transactions (
			id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
			origin, status, amount, merchant_raw, merchant_clean, category_id,
			transfer_linked_transaction_id, transaction_date, user_notes, is_cleared
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING is_transfer, created_at
	`,
		t.ID, t.WorkspaceID, t.AccountID, t.ParentTransactionID, t.ExternalTransactionID,
		string(t.Origin), string(t.Status), t.Amount.Round(2), t.MerchantRaw, t.MerchantClean, t.CategoryID,
		t.TransferLinkedTransactionID, t.TransactionDate.UTC().Format("2006-01-02"), t.UserNotes, t.IsCleared,
	).Scan(&t.IsTransfer, &t.CreatedAt)
}

func (r *pgRepo) GetByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Transaction, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, err
	}
	var t Transaction
	var originStr, statusStr string
	err = tx.QueryRow(ctx, `
		SELECT id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
		       origin, status, amount, merchant_raw, merchant_clean, category_id,
		       transfer_linked_transaction_id, is_transfer, settled_delta, transaction_date, user_notes,
		       is_cleared, created_at
		FROM transactions
		WHERE id = $1 AND workspace_id = $2
	`, id, wsID).Scan(
		&t.ID, &t.WorkspaceID, &t.AccountID, &t.ParentTransactionID, &t.ExternalTransactionID,
		&originStr, &statusStr, &t.Amount, &t.MerchantRaw, &t.MerchantClean, &t.CategoryID,
		&t.TransferLinkedTransactionID, &t.IsTransfer, &t.SettledDelta, &t.TransactionDate, &t.UserNotes,
		&t.IsCleared, &t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, err
	}
	t.Origin = Origin(originStr)
	t.Status = Status(statusStr)

	splits, err := r.listSplitsByParent(ctx, tx, wsID, t.ID)
	if err != nil {
		return nil, err
	}
	t.Splits = splits
	return &t, nil
}

func (r *pgRepo) listSplitsByParent(ctx context.Context, tx pgx.Tx, wsID, parentID uuid.UUID) ([]Transaction, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
		       origin, status, amount, merchant_raw, merchant_clean, category_id,
		       transfer_linked_transaction_id, is_transfer, settled_delta, transaction_date, user_notes,
		       is_cleared, created_at
		FROM transactions
		WHERE parent_transaction_id = $1 AND workspace_id = $2
		ORDER BY created_at ASC
	`, parentID, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var splits []Transaction
	for rows.Next() {
		var s Transaction
		var originStr, statusStr string
		if err := rows.Scan(
			&s.ID, &s.WorkspaceID, &s.AccountID, &s.ParentTransactionID, &s.ExternalTransactionID,
			&originStr, &statusStr, &s.Amount, &s.MerchantRaw, &s.MerchantClean, &s.CategoryID,
			&s.TransferLinkedTransactionID, &s.IsTransfer, &s.SettledDelta, &s.TransactionDate, &s.UserNotes,
			&s.IsCleared, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		s.Origin = Origin(originStr)
		s.Status = Status(statusStr)
		splits = append(splits, s)
	}
	return splits, rows.Err()
}

func (r *pgRepo) List(ctx context.Context, tx pgx.Tx, filter TransactionFilter) ([]Transaction, int, error) {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return nil, 0, err
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}

	where := []string{"workspace_id = $1", "parent_transaction_id IS NULL"}
	args := []any{wsID}
	argIdx := 2

	if filter.AccountID != nil {
		where = append(where, fmt.Sprintf("account_id = $%d", argIdx))
		args = append(args, *filter.AccountID)
		argIdx++
	}
	if filter.CategoryID != nil {
		where = append(where, fmt.Sprintf("(category_id = $%d OR EXISTS (SELECT 1 FROM transactions c WHERE c.parent_transaction_id = transactions.id AND c.workspace_id = $1 AND c.category_id = $%d))", argIdx, argIdx))
		args = append(args, *filter.CategoryID)
		argIdx++
	}
	if filter.Status != nil && *filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *filter.Status)
		argIdx++
	}
	if filter.Month != nil {
		m := filter.Month.UTC()
		start := time.Date(m.Year(), m.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		where = append(where, fmt.Sprintf("transaction_date >= $%d AND transaction_date < $%d", argIdx, argIdx+1))
		args = append(args, start.Format("2006-01-02"), end.Format("2006-01-02"))
		argIdx += 2
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM transactions WHERE %s", whereClause)
	if err := tx.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	listArgs := append(args, filter.Limit, offset)
	listQuery := fmt.Sprintf(`
		SELECT id, workspace_id, account_id, parent_transaction_id, external_transaction_id,
		       origin, status, amount, merchant_raw, merchant_clean, category_id,
		       transfer_linked_transaction_id, is_transfer, settled_delta, transaction_date, user_notes,
		       is_cleared, created_at
		FROM transactions
		WHERE %s
		ORDER BY transaction_date DESC, created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)

	rows, err := tx.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []Transaction
	for rows.Next() {
		var t Transaction
		var originStr, statusStr string
		if err := rows.Scan(
			&t.ID, &t.WorkspaceID, &t.AccountID, &t.ParentTransactionID, &t.ExternalTransactionID,
			&originStr, &statusStr, &t.Amount, &t.MerchantRaw, &t.MerchantClean, &t.CategoryID,
			&t.TransferLinkedTransactionID, &t.IsTransfer, &t.SettledDelta, &t.TransactionDate, &t.UserNotes,
			&t.IsCleared, &t.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		t.Origin = Origin(originStr)
		t.Status = Status(statusStr)
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	for i := range result {
		splits, err := r.listSplitsByParent(ctx, tx, wsID, result[i].ID)
		if err != nil {
			return nil, 0, err
		}
		result[i].Splits = splits
	}

	return result, total, nil
}

func (r *pgRepo) Update(ctx context.Context, tx pgx.Tx, t *Transaction) error {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE transactions
		SET category_id = $3,
		    merchant_clean = $4,
		    user_notes = $5,
		    status = $6,
		    is_cleared = $7,
		    amount = $8
		WHERE id = $1 AND workspace_id = $2
	`, t.ID, wsID, t.CategoryID, t.MerchantClean, t.UserNotes, string(t.Status), t.IsCleared, t.Amount.Round(2))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}
	return nil
}

func (r *pgRepo) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	wsID, err := db.ActiveWorkspaceID(ctx, tx)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM transactions WHERE id = $1 AND workspace_id = $2`, id, wsID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
