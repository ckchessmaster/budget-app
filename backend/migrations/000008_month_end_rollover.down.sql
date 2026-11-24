DROP POLICY IF EXISTS monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments;
DROP POLICY IF EXISTS cc_debt_interlocks_isolation ON credit_card_debt_interlocks;
DROP POLICY IF EXISTS month_end_rollover_runs_isolation ON month_end_rollover_runs;

DROP TABLE IF EXISTS monthly_rollover_adjustments;
DROP TABLE IF EXISTS credit_card_debt_interlocks;
DROP TABLE IF EXISTS month_end_rollover_runs;

ALTER TABLE monthly_category_budgets
    DROP COLUMN IF EXISTS carryover_balance;
