ALTER TABLE monthly_category_budgets
    ADD COLUMN IF NOT EXISTS carryover_balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00;

CREATE TABLE IF NOT EXISTS month_end_rollover_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_month DATE NOT NULL,
    target_month DATE NOT NULL,
    total_rolled_over NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    total_swept_to_categories NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    total_swept_to_pool NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    total_cash_deficit NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    total_cc_deficit NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    executed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, source_month)
);
CREATE INDEX IF NOT EXISTS idx_month_end_rollover_runs_ws_target
    ON month_end_rollover_runs (workspace_id, target_month);

CREATE TABLE IF NOT EXISTS credit_card_debt_interlocks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    account_id UUID REFERENCES accounts(id) ON DELETE SET NULL,
    source_month DATE NOT NULL,
    target_month DATE NOT NULL,
    deficit_amount NUMERIC(12, 2) NOT NULL CHECK (deficit_amount > 0),
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'RESOLVED_KEEP_DEDUCTION', 'RESOLVED_CONVERT_TO_DEBT')),
    resolved_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE NULLS NOT DISTINCT (workspace_id, account_id, source_month)
);
CREATE INDEX IF NOT EXISTS idx_cc_debt_interlocks_ws_target
    ON credit_card_debt_interlocks (workspace_id, target_month, status);

CREATE TABLE IF NOT EXISTS monthly_rollover_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_month DATE NOT NULL,
    target_month DATE NOT NULL,
    category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
    interlock_id UUID REFERENCES credit_card_debt_interlocks(id) ON DELETE SET NULL,
    adjustment_type VARCHAR(32) NOT NULL
        CHECK (adjustment_type IN ('SWEEP_TO_POOL', 'CASH_DEFICIT_DEDUCTION', 'CC_DEFICIT_DEDUCTION')),
    amount NUMERIC(12, 2) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_monthly_rollover_adjustments_ws_target
    ON monthly_rollover_adjustments (workspace_id, target_month)
    WHERE is_active = TRUE;

ALTER TABLE month_end_rollover_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE credit_card_debt_interlocks ENABLE ROW LEVEL SECURITY;
ALTER TABLE monthly_rollover_adjustments ENABLE ROW LEVEL SECURITY;

ALTER TABLE month_end_rollover_runs FORCE ROW LEVEL SECURITY;
ALTER TABLE credit_card_debt_interlocks FORCE ROW LEVEL SECURITY;
ALTER TABLE monthly_rollover_adjustments FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS month_end_rollover_runs_isolation ON month_end_rollover_runs;
CREATE POLICY month_end_rollover_runs_isolation ON month_end_rollover_runs FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS cc_debt_interlocks_isolation ON credit_card_debt_interlocks;
CREATE POLICY cc_debt_interlocks_isolation ON credit_card_debt_interlocks FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments;
CREATE POLICY monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
