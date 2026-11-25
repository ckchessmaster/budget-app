DROP INDEX IF EXISTS idx_transactions_needs_review;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS needs_review,
    DROP COLUMN IF EXISTS applied_rule_id,
    DROP COLUMN IF EXISTS categorized_by;

DROP POLICY IF EXISTS workspace_isolation_categorization_rules ON categorization_rules;
DROP INDEX IF EXISTS idx_categorization_rules_ws_priority;
DROP TABLE IF EXISTS categorization_rules;
