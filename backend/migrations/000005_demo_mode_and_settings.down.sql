DROP POLICY IF EXISTS demo_sf_transactions_isolation ON demo_simplefin_transactions;
DROP POLICY IF EXISTS demo_sf_accounts_isolation ON demo_simplefin_accounts;
DROP POLICY IF EXISTS demo_sf_state_isolation ON demo_simplefin_state;

DROP TABLE IF EXISTS demo_simplefin_transactions;
DROP TABLE IF EXISTS demo_simplefin_accounts;
DROP TABLE IF EXISTS demo_simplefin_state;

CREATE OR REPLACE FUNCTION current_app_user_workspace_ids() RETURNS SETOF UUID AS $$
    SELECT workspace_id FROM workspace_members WHERE user_id = current_app_user_id();
$$ LANGUAGE SQL STABLE SECURITY DEFINER;

DROP POLICY IF EXISTS user_preferences_isolation ON user_preferences;
DROP TABLE IF EXISTS user_preferences;

ALTER TABLE simplefin_connections
    DROP COLUMN IF EXISTS last_sync_warnings;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS settled_delta;

ALTER TABLE workspaces
    DROP COLUMN IF EXISTS is_demo;
