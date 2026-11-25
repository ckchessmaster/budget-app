DROP INDEX IF EXISTS idx_categories_workspace_held_in_account;

ALTER TABLE categories
    DROP COLUMN IF EXISTS held_in_account_id;
