package activities

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ckchessmaster/budget-app/backend/internal/account"
	temporalinternal "github.com/ckchessmaster/budget-app/backend/internal/temporal"
	"github.com/ckchessmaster/budget-app/backend/internal/transaction"
	"github.com/ckchessmaster/budget-app/backend/pkg/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.temporal.io/sdk/temporal"
)

func replaySeed(t *testing.T, adminPool *pgxpool.Pool, wsID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()

	var accountID, categoryID uuid.UUID
	ctx := context.Background()
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO accounts (workspace_id, name, type, current_balance, is_on_budget)
		VALUES ($1, 'Replay Checking', 'checking', 2000.00, TRUE)
	`, wsID); err != nil {
		t.Fatalf("failed to seed account: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		INSERT INTO categories (workspace_id, name, group_name, monthly_budget, sort_order)
		VALUES ($1, 'Groceries', 'Living', 400.00, 1)
		RETURNING id
	`, wsID).Scan(&categoryID); err != nil {
		t.Fatalf("failed to seed category: %v", err)
	}
	if err := adminPool.QueryRow(ctx, `
		SELECT id FROM accounts WHERE workspace_id = $1 AND name = 'Replay Checking'
	`, wsID).Scan(&accountID); err != nil {
		t.Fatalf("failed to load seeded account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DELETE FROM categories WHERE id = $1", categoryID)
	})
	return accountID, categoryID
}

func replayBatchItems(accountID, categoryID uuid.UUID, n int) []temporalinternal.ReplayItem {
	items := make([]temporalinternal.ReplayItem, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, temporalinternal.ReplayItem{
			ClientID:        fmt.Sprintf("replay-%d-%s", i, accountID.String()[:8]),
			AccountID:       accountID,
			Amount:          decimal.NewFromFloat(-12.50),
			MerchantRaw:     "Corner Store",
			CategoryID:      &categoryID,
			TransactionDate: "2026-11-18",
		})
	}
	return items
}

func countReplayRows(t *testing.T, appPool *pgxpool.Pool, userID, wsID uuid.UUID) int {
	t.Helper()

	var count int
	err := db.WithWorkspaceTx(context.Background(), appPool, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM transactions WHERE workspace_id = $1`, wsID).Scan(&count)
	})
	if err != nil {
		t.Fatalf("failed to count transactions: %v", err)
	}
	return count
}

func accountBalance(t *testing.T, appPool *pgxpool.Pool, userID, accountID uuid.UUID) decimal.Decimal {
	t.Helper()

	var balance decimal.Decimal
	err := db.WithWorkspaceTx(context.Background(), appPool, userID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT current_balance FROM accounts WHERE id = $1`, accountID).Scan(&balance)
	})
	if err != nil {
		t.Fatalf("failed to read account balance: %v", err)
	}
	return balance
}

// TestReplayTransactionItemActivityLandsValidItemsAndRejectsOne is the acceptance case: a
// batch of five where one entry cannot be applied lands four, and the fifth comes back with
// a machine-readable terminal reason instead of rolling the successes away with it.
func TestReplayTransactionItemActivityLandsValidItemsAndRejectsOne(t *testing.T) {
	adminPool, appPool := openScopeTestDB(t)
	ctx := context.Background()

	userID, wsID := provisionWorkspace(t, adminPool, appPool)
	accountID, categoryID := replaySeed(t, adminPool, wsID)

	acts := &ReplayActivities{
		Pool:         appPool,
		Transactions: transaction.NewService(transaction.NewRepo(), account.NewRepo()),
	}

	items := replayBatchItems(accountID, categoryID, 4)
	// An account id that does not exist in this workspace. The foreign key alone would not
	// catch a row from another workspace, because Postgres does not apply row level security
	// to foreign key lookups — the activity checks membership itself.
	ghost := temporalinternal.ReplayItem{
		ClientID:        "replay-ghost",
		AccountID:       uuid.New(),
		Amount:          decimal.NewFromFloat(-12.50),
		MerchantRaw:     "Corner Store",
		TransactionDate: "2026-11-18",
	}
	items = append(items, ghost)

	scope := temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}

	applied := 0
	var rejected *temporal.ApplicationError
	for _, item := range items {
		_, err := acts.ReplayTransactionItemActivity(ctx, ReplayItemInput{SyncScope: scope, Item: item})
		if err == nil {
			applied++
			continue
		}
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) && appErr.NonRetryable() {
			rejected = appErr
			continue
		}
		t.Fatalf("item %s failed with an unexpected error: %v", item.ClientID, err)
	}

	if applied != 4 {
		t.Fatalf("applied = %d, want the four valid items", applied)
	}
	if rejected == nil {
		t.Fatalf("expected the unknown-account item to be rejected terminally")
	}
	if rejected.Type() != transaction.ReasonUnknownAccount {
		t.Fatalf("rejection type = %q, want %q", rejected.Type(), transaction.ReasonUnknownAccount)
	}
	if got := countReplayRows(t, appPool, userID, wsID); got != 4 {
		t.Fatalf("row count = %d, want 4", got)
	}
}

// TestReplayTransactionItemActivityReplayCreatesNoDuplicates is the property the whole task
// rests on: at-least-once delivery plus a client key must equal exactly-once effect. The
// balance is asserted alongside the row count because a duplicate that skips the INSERT but
// still adjusts the balance would leave the ledger quietly wrong.
func TestReplayTransactionItemActivityReplayCreatesNoDuplicates(t *testing.T) {
	adminPool, appPool := openScopeTestDB(t)
	ctx := context.Background()

	userID, wsID := provisionWorkspace(t, adminPool, appPool)
	accountID, categoryID := replaySeed(t, adminPool, wsID)

	acts := &ReplayActivities{
		Pool:         appPool,
		Transactions: transaction.NewService(transaction.NewRepo(), account.NewRepo()),
	}
	scope := temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID}
	items := replayBatchItems(accountID, categoryID, 3)

	for _, item := range items {
		if _, err := acts.ReplayTransactionItemActivity(ctx, ReplayItemInput{SyncScope: scope, Item: item}); err != nil {
			t.Fatalf("first pass for %s: %v", item.ClientID, err)
		}
	}
	rowsAfterFirst := countReplayRows(t, appPool, userID, wsID)
	balanceAfterFirst := accountBalance(t, appPool, userID, accountID)
	if rowsAfterFirst != 3 {
		t.Fatalf("rows after first pass = %d, want 3", rowsAfterFirst)
	}

	for _, item := range items {
		res, err := acts.ReplayTransactionItemActivity(ctx, ReplayItemInput{SyncScope: scope, Item: item})
		if err != nil {
			t.Fatalf("second pass for %s must succeed, got %v", item.ClientID, err)
		}
		if !res.Duplicate {
			t.Fatalf("item %s reported applied again on the second pass", item.ClientID)
		}
	}

	if got := countReplayRows(t, appPool, userID, wsID); got != rowsAfterFirst {
		t.Fatalf("rows after replay = %d, want unchanged %d", got, rowsAfterFirst)
	}
	if got := accountBalance(t, appPool, userID, accountID); !got.Equal(balanceAfterFirst) {
		t.Fatalf("balance after replay = %s, want unchanged %s", got, balanceAfterFirst)
	}
}

// TestReplayTransactionItemActivityRejectsBadDatesWithoutDefaulting guards the fallback the
// synchronous handler used to apply: an unparseable date became "today", which moved the
// transaction into whichever month the retry happened to land in.
func TestReplayTransactionItemActivityRejectsBadDatesWithoutDefaulting(t *testing.T) {
	adminPool, appPool := openScopeTestDB(t)
	ctx := context.Background()

	userID, wsID := provisionWorkspace(t, adminPool, appPool)
	accountID, categoryID := replaySeed(t, adminPool, wsID)

	acts := &ReplayActivities{
		Pool:         appPool,
		Transactions: transaction.NewService(transaction.NewRepo(), account.NewRepo()),
	}

	items := replayBatchItems(accountID, categoryID, 1)
	items[0].TransactionDate = "not-a-date"

	_, err := acts.ReplayTransactionItemActivity(ctx, ReplayItemInput{
		SyncScope: temporalinternal.SyncScope{WorkspaceID: wsID, UserID: userID},
		Item:      items[0],
	})
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want a terminal rejection", err)
	}
	if appErr.Type() != transaction.ReasonInvalidTransactionDate {
		t.Fatalf("rejection type = %q, want %q", appErr.Type(), transaction.ReasonInvalidTransactionDate)
	}
	if got := countReplayRows(t, appPool, userID, wsID); got != 0 {
		t.Fatalf("row count = %d, want nothing written for a rejected item", got)
	}
}
