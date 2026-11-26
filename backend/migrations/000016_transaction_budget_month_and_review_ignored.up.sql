ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS budget_month DATE,
    ADD COLUMN IF NOT EXISTS review_ignored BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_transactions_budget_month
    ON transactions (workspace_id, budget_month)
    WHERE budget_month IS NOT NULL;
