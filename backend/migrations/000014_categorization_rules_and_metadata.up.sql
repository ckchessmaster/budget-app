CREATE TABLE IF NOT EXISTS categorization_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    priority INT NOT NULL DEFAULT 0,
    match_type VARCHAR(16) NOT NULL DEFAULT 'CONTAINS' CHECK (match_type IN ('CONTAINS', 'EXACT')),
    merchant_pattern VARCHAR(255) NOT NULL,
    account_id UUID REFERENCES accounts(id) ON DELETE CASCADE,
    direction VARCHAR(16) NOT NULL DEFAULT 'ANY' CHECK (direction IN ('ANY', 'EXPENSE', 'INFLOW')),
    min_amount DECIMAL(14, 2),
    max_amount DECIMAL(14, 2),
    category_id UUID REFERENCES categories(id) ON DELETE CASCADE,
    planned_income_name VARCHAR(150),
    rename_merchant VARCHAR(255),
    require_review BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT categorization_rules_target_chk CHECK (
        (category_id IS NOT NULL) <> (planned_income_name IS NOT NULL AND planned_income_name <> '')
    )
);

CREATE INDEX IF NOT EXISTS idx_categorization_rules_ws_priority
    ON categorization_rules (workspace_id, priority ASC, created_at ASC);

ALTER TABLE categorization_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE categorization_rules FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS workspace_isolation_categorization_rules ON categorization_rules;
CREATE POLICY workspace_isolation_categorization_rules ON categorization_rules
    FOR ALL
    USING (workspace_id IN (SELECT id FROM workspaces))
    WITH CHECK (workspace_id IN (SELECT id FROM workspaces));

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS categorized_by VARCHAR(16) NOT NULL DEFAULT 'MANUAL'
        CHECK (categorized_by IN ('MANUAL', 'RULE')),
    ADD COLUMN IF NOT EXISTS applied_rule_id UUID REFERENCES categorization_rules(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS needs_review BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_transactions_needs_review
    ON transactions (workspace_id)
    WHERE needs_review = TRUE;
