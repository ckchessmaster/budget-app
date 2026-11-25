DROP INDEX IF EXISTS idx_transactions_workspace_planned_income;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS planned_income_id;

ALTER TABLE monthly_planned_incomes
    DROP CONSTRAINT IF EXISTS monthly_planned_incomes_amount_check;

ALTER TABLE monthly_planned_incomes
    ADD CONSTRAINT monthly_planned_incomes_amount_check CHECK (amount > 0);
