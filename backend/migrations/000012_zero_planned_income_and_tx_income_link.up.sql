-- Allow $0.00 planned income sources (e.g. "Unplanned Income") and allow
-- transactions to be linked directly to a planned income source.
ALTER TABLE monthly_planned_incomes
    DROP CONSTRAINT IF EXISTS monthly_planned_incomes_amount_check;

ALTER TABLE monthly_planned_incomes
    ADD CONSTRAINT monthly_planned_incomes_amount_check CHECK (amount >= 0);

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS planned_income_id UUID REFERENCES monthly_planned_incomes(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_transactions_workspace_planned_income
    ON transactions (workspace_id, planned_income_id)
    WHERE planned_income_id IS NOT NULL;
