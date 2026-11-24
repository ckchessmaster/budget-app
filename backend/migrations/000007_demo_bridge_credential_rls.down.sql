-- Restore the single user-scoped arm and drop the bridge settings helpers.
DROP POLICY IF EXISTS demo_sf_transactions_isolation ON demo_simplefin_transactions;
CREATE POLICY demo_sf_transactions_isolation ON demo_simplefin_transactions FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS demo_sf_accounts_isolation ON demo_simplefin_accounts;
CREATE POLICY demo_sf_accounts_isolation ON demo_simplefin_accounts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS demo_sf_state_isolation ON demo_simplefin_state;
CREATE POLICY demo_sf_state_isolation ON demo_simplefin_state FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP FUNCTION IF EXISTS current_demo_bridge_password();
DROP FUNCTION IF EXISTS current_demo_bridge_username();
DROP FUNCTION IF EXISTS current_demo_bridge_workspace_id();
