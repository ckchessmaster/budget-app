DROP INDEX IF EXISTS idx_transactions_budget_month;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS review_ignored,
    DROP COLUMN IF EXISTS budget_month;
