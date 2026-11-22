package demo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ckchessmaster/budget-app/backend/internal/workspace"
	"github.com/ckchessmaster/budget-app/backend/pkg/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func monthSafeDate(now time.Time, daysAgo int) time.Time {
	u := now.UTC()
	monthStart := time.Date(u.Year(), u.Month(), 1, 12, 0, 0, 0, time.UTC)
	target := time.Date(u.Year(), u.Month(), u.Day(), 12, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
	if target.Before(monthStart) {
		return monthStart
	}
	return target
}

func buildMockAccessURL(baseURL string, wsID uuid.UUID, username, password string) (string, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = "http://127.0.0.1:8080"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid mock simplefin base url: %w", err)
	}
	parsed.User = url.UserPassword(username, password)
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/mock-simplefin/" + wsID.String()
	return parsed.String(), nil
}

// SeedDemoWorkspaceTx creates and seeds an isolated demo workspace for userID inside tx.
func SeedDemoWorkspaceTx(
	ctx context.Context,
	tx pgx.Tx,
	userID uuid.UUID,
	mockBaseURL string,
	vault crypto.Vault,
) (uuid.UUID, error) {
	demoWSID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO workspaces (id, name, is_demo)
		VALUES ($1, 'Demo Sandbox Workspace', TRUE)
	`, demoWSID); err != nil {
		return uuid.Nil, fmt.Errorf("create demo workspace: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, demoWSID, userID); err != nil {
		return uuid.Nil, fmt.Errorf("add demo workspace member: %w", err)
	}

	domain := os.Getenv("RECEIPT_DOMAIN")
	if domain == "" {
		domain = "receipts.yourdomain.com"
	}
	rawUUID := strings.ReplaceAll(demoWSID.String(), "-", "")
	receiptAlias := fmt.Sprintf("demo_%s@%s", rawUUID[:8], domain)
	if _, err := tx.Exec(ctx, `
		INSERT INTO workspace_settings (workspace_id, inbound_receipt_alias)
		VALUES ($1, $2)
	`, demoWSID, receiptAlias); err != nil {
		return uuid.Nil, fmt.Errorf("create demo workspace settings: %w", err)
	}

	if err := workspace.SeedDefaultCategories(ctx, tx, demoWSID); err != nil {
		return uuid.Nil, fmt.Errorf("seed demo categories: %w", err)
	}

	// Set balanced zero-based monthly budgets on the 10 default categories (sum = $4,200.00)
	categoryBudgets := map[string]string{
		"Rent/Mortgage":    "1650.00",
		"Utilities":        "220.00",
		"Groceries":        "600.00",
		"Transportation":   "250.00",
		"Dining Out":       "320.00",
		"Entertainment":    "180.00",
		"Personal Care":    "130.00",
		"Auto Maintenance": "150.00",
		"Medical Buffer":   "200.00",
		"Emergency Fund":   "500.00",
	}
	for name, amt := range categoryBudgets {
		if _, err := tx.Exec(ctx, `
			UPDATE categories SET monthly_budget = $3
			WHERE workspace_id = $1 AND name = $2
		`, demoWSID, name, amt); err != nil {
			return uuid.Nil, fmt.Errorf("update budget for category %s: %w", name, err)
		}
	}

	// Add 11th category for Debt Paydown credit card ($1,000.00 -> total allocated $5,200.00)
	debtPayoffCatID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO categories (
			id, workspace_id, name, group_name, monthly_budget,
			rollover_mode, target_cadence, sort_order
		) VALUES (
			$1, $2, 'Credit Card Payoff', 'Debt Paydown', 1000.00,
			'SWEEP', 'NONE', 11
		)
	`, debtPayoffCatID, demoWSID); err != nil {
		return uuid.Nil, fmt.Errorf("insert debt payoff category: %w", err)
	}

	// Load category IDs by name for starter transactions
	catIDs := make(map[string]uuid.UUID)
	rows, err := tx.Query(ctx, `SELECT id, name FROM categories WHERE workspace_id = $1`, demoWSID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("query demo categories: %w", err)
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return uuid.Nil, err
		}
		catIDs[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}

	now := time.Now().UTC()
	ym := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Freeze current month baselines so Budget view matches planned income immediately ($5,200.00)
	if _, err := tx.Exec(ctx, `
		INSERT INTO monthly_category_budgets (workspace_id, category_id, year_month, original_planned)
		SELECT workspace_id, id, $2::date, monthly_budget
		FROM categories
		WHERE workspace_id = $1
		ON CONFLICT (workspace_id, category_id, year_month) DO NOTHING
	`, demoWSID, ym); err != nil {
		return uuid.Nil, fmt.Errorf("initialize demo month baselines: %w", err)
	}

	incomes := []struct {
		name   string
		amount string
	}{
		{"Acme Corp Paycheck #1", "2600.00"},
		{"Acme Corp Paycheck #2", "2600.00"},
	}
	for _, inc := range incomes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO monthly_planned_incomes (workspace_id, year_month, name, amount)
			VALUES ($1, $2::date, $3, $4)
		`, demoWSID, ym, inc.name, inc.amount); err != nil {
			return uuid.Nil, fmt.Errorf("insert demo planned income: %w", err)
		}
	}

	// Set up mock SimpleFIN credentials & encrypted access URL
	authUser, err := randomHex(8)
	if err != nil {
		return uuid.Nil, err
	}
	authPass, err := randomHex(16)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO demo_simplefin_state (workspace_id, auth_username, auth_password, fault_mode)
		VALUES ($1, $2, $3, 'NONE')
	`, demoWSID, authUser, authPass); err != nil {
		return uuid.Nil, fmt.Errorf("insert demo simplefin state: %w", err)
	}

	accessURL, err := buildMockAccessURL(mockBaseURL, demoWSID, authUser, authPass)
	if err != nil {
		return uuid.Nil, err
	}
	encAccessURL, err := vault.Encrypt(accessURL)
	if err != nil {
		return uuid.Nil, fmt.Errorf("encrypt demo access url: %w", err)
	}
	encClaimToken, err := vault.Encrypt("demo-setup-token-" + demoWSID.String())
	if err != nil {
		return uuid.Nil, fmt.Errorf("encrypt demo claim token: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO simplefin_connections (workspace_id, encrypted_claim_token, encrypted_access_url, last_synced_at)
		VALUES ($1, $2, $3, NOW())
	`, demoWSID, encClaimToken, encAccessURL); err != nil {
		return uuid.Nil, fmt.Errorf("insert demo simplefin connection: %w", err)
	}

	// Seed 4 linked SimpleFIN accounts
	type seedAcct struct {
		extID           string
		name            string
		acctType        string
		balance         string
		cardMode        string
		initialDebt     string
		apr             string
		dueDay          *int
		debtPayoffCatID *uuid.UUID
		internalID      uuid.UUID
	}
	due15 := 15
	seedAccounts := []seedAcct{
		{
			extID:      "demo-checking-001",
			name:       "Demo Checking",
			acctType:   "checking",
			balance:    "4250.00",
			cardMode:   "PAY_IN_FULL",
			internalID: uuid.New(),
		},
		{
			extID:      "demo-savings-001",
			name:       "Demo High-Yield Savings",
			acctType:   "savings",
			balance:    "8500.00",
			cardMode:   "PAY_IN_FULL",
			internalID: uuid.New(),
		},
		{
			extID:      "demo-cc-rewards-001",
			name:       "Demo Everyday Rewards",
			acctType:   "credit_card",
			balance:    "-645.20",
			cardMode:   "PAY_IN_FULL",
			internalID: uuid.New(),
		},
		{
			extID:           "demo-cc-legacy-001",
			name:            "Demo Legacy Card",
			acctType:        "credit_card",
			balance:         "-2400.00",
			cardMode:        "DEBT_PAYDOWN",
			initialDebt:     "2400.00",
			apr:             "19.99",
			dueDay:          &due15,
			debtPayoffCatID: &debtPayoffCatID,
			internalID:      uuid.New(),
		},
	}

	acctIDByExt := make(map[string]uuid.UUID, len(seedAccounts))
	for _, a := range seedAccounts {
		acctIDByExt[a.extID] = a.internalID
		if _, err := tx.Exec(ctx, `
			INSERT INTO demo_simplefin_accounts (
				workspace_id, external_account_id, name, currency, balance, account_type, card_mode
			) VALUES ($1, $2, $3, 'USD', $4, $5, $6)
		`, demoWSID, a.extID, a.name, a.balance, a.acctType, a.cardMode); err != nil {
			return uuid.Nil, fmt.Errorf("insert demo simplefin account %s: %w", a.name, err)
		}

		initDebt := decimal.Zero
		if a.initialDebt != "" {
			initDebt, _ = decimal.NewFromString(a.initialDebt)
		}
		apr := decimal.Zero
		if a.apr != "" {
			apr, _ = decimal.NewFromString(a.apr)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (
				id, workspace_id, external_account_id, name, type, current_balance,
				is_on_budget, card_mode, initial_debt_baseline, apr_percentage,
				statement_due_day, debt_payoff_category_id
			) VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, $8, $9, $10, $11)
		`, a.internalID, demoWSID, a.extID, a.name, a.acctType, a.balance,
			a.cardMode, initDebt, apr, a.dueDay, a.debtPayoffCatID); err != nil {
			return uuid.Nil, fmt.Errorf("insert demo account %s: %w", a.name, err)
		}
	}

	// Seed starter transactions (synced in both fake SimpleFIN bank & workspace ledger)
	type seedTx struct {
		extAcctID string
		extTxID   string
		daysAgo   int
		amount    string
		rawDesc   string
		cleanDesc string
		catName   string
		pending   bool
	}
	starterTxs := []seedTx{
		{
			extAcctID: "demo-checking-001",
			extTxID:   "demo-seed-tx-1",
			daysAgo:   10,
			amount:    "2600.00",
			rawDesc:   "DIRECT DEP ACME CORP PAYROLL",
			cleanDesc: "Acme Corp Payroll",
			pending:   false,
		},
		{
			extAcctID: "demo-checking-001",
			extTxID:   "demo-seed-tx-2",
			daysAgo:   9,
			amount:    "-1650.00",
			rawDesc:   "ACH DEBIT RIVERSTONE APTS RENT",
			cleanDesc: "Riverstone Apts Rent",
			catName:   "Rent/Mortgage",
			pending:   false,
		},
		{
			extAcctID: "demo-checking-001",
			extTxID:   "demo-seed-tx-3",
			daysAgo:   6,
			amount:    "-142.50",
			rawDesc:   "CITY POWER & WATER AUTOPAY",
			cleanDesc: "City Power & Water",
			catName:   "Utilities",
			pending:   false,
		},
		{
			extAcctID: "demo-cc-rewards-001",
			extTxID:   "demo-seed-tx-4",
			daysAgo:   4,
			amount:    "-184.70",
			rawDesc:   "POS DEBIT TRADER JOES #104",
			cleanDesc: "Trader Joes",
			catName:   "Groceries",
			pending:   false,
		},
		{
			extAcctID: "demo-cc-rewards-001",
			extTxID:   "demo-seed-tx-5",
			daysAgo:   2,
			amount:    "-14.50",
			rawDesc:   "SQ *BLUE BOTTLE COFFEE",
			cleanDesc: "Blue Bottle Coffee",
			catName:   "Dining Out",
			pending:   false,
		},
		{
			extAcctID: "demo-cc-rewards-001",
			extTxID:   "demo-seed-tx-6",
			daysAgo:   1,
			amount:    "-40.00",
			rawDesc:   "BURGER BAR",
			cleanDesc: "Burger Bar",
			catName:   "Dining Out",
			pending:   true,
		},
		{
			extAcctID: "demo-cc-rewards-001",
			extTxID:   "demo-seed-tx-7",
			daysAgo:   0,
			amount:    "-52.00",
			rawDesc:   "SHELL OIL 57442",
			cleanDesc: "Shell Oil",
			catName:   "Transportation",
			pending:   true,
		},
	}

	for _, st := range starterTxs {
		postedAt := monthSafeDate(now, st.daysAgo)
		if _, err := tx.Exec(ctx, `
			INSERT INTO demo_simplefin_transactions (
				workspace_id, external_account_id, external_transaction_id,
				posted_at, amount, description, pending
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, demoWSID, st.extAcctID, st.extTxID, postedAt, st.amount, st.rawDesc, st.pending); err != nil {
			return uuid.Nil, fmt.Errorf("insert demo simplefin tx %s: %w", st.extTxID, err)
		}

		status := "POSTED"
		isCleared := true
		if st.pending {
			status = "PENDING"
			isCleared = false
		}
		var catID *uuid.UUID
		if st.catName != "" {
			if cid, ok := catIDs[st.catName]; ok {
				catCopy := cid
				catID = &catCopy
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (
				workspace_id, account_id, external_transaction_id, origin, status,
				amount, merchant_raw, merchant_clean, category_id, transaction_date, is_cleared
			) VALUES ($1, $2, $3, 'BANK_FEED', $4, $5, $6, $7, $8, $9, $10)
		`, demoWSID, acctIDByExt[st.extAcctID], st.extTxID, status, st.amount,
			st.rawDesc, st.cleanDesc, catID, postedAt.Format("2006-01-02"), isCleared); err != nil {
			return uuid.Nil, fmt.Errorf("insert demo ledger tx %s: %w", st.extTxID, err)
		}
	}

	return demoWSID, nil
}
