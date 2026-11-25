UPDATE accounts
SET is_on_budget = (type NOT IN ('investment', 'loan'));

ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS held_in_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_categories_workspace_held_in_account
    ON categories (workspace_id, held_in_account_id)
    WHERE held_in_account_id IS NOT NULL;
