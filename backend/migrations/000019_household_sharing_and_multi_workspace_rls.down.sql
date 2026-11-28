-- Revert Household Sharing, Multi-Workspace Switching & Family Invites

DROP POLICY IF EXISTS workspace_isolation_categorization_rules ON categorization_rules;
CREATE POLICY workspace_isolation_categorization_rules ON categorization_rules
    FOR ALL
    USING (workspace_id IN (SELECT id FROM workspaces))
    WITH CHECK (workspace_id IN (SELECT id FROM workspaces));

DROP POLICY IF EXISTS notification_cooldowns_isolation ON notification_cooldowns;
CREATE POLICY notification_cooldowns_isolation ON notification_cooldowns FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
) WITH CHECK (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments;
CREATE POLICY monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS cc_debt_interlocks_isolation ON credit_card_debt_interlocks;
CREATE POLICY cc_debt_interlocks_isolation ON credit_card_debt_interlocks FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS month_end_rollover_runs_isolation ON month_end_rollover_runs;
CREATE POLICY month_end_rollover_runs_isolation ON month_end_rollover_runs FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS simplefin_isolation ON simplefin_connections;
CREATE POLICY simplefin_isolation ON simplefin_connections FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS receipts_isolation ON receipts;
CREATE POLICY receipts_isolation ON receipts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS transactions_isolation ON transactions;
CREATE POLICY transactions_isolation ON transactions FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS reallocations_isolation ON budget_reallocations;
CREATE POLICY reallocations_isolation ON budget_reallocations FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS incomes_isolation ON monthly_planned_incomes;
CREATE POLICY incomes_isolation ON monthly_planned_incomes FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS budgets_isolation ON monthly_category_budgets;
CREATE POLICY budgets_isolation ON monthly_category_budgets FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS categories_isolation ON categories;
CREATE POLICY categories_isolation ON categories FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS accounts_isolation ON accounts;
CREATE POLICY accounts_isolation ON accounts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS settings_isolation ON workspace_settings;
CREATE POLICY settings_isolation ON workspace_settings FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS invitations_workspace_isolation ON workspace_invitations;
DROP TABLE IF EXISTS workspace_invitations;
DROP TYPE IF EXISTS invite_status;

DROP POLICY IF EXISTS workspace_roster_isolation ON workspace_roster;
DROP TABLE IF EXISTS workspace_roster;

DROP POLICY IF EXISTS workspace_isolation ON workspaces;
CREATE POLICY workspace_isolation ON workspaces FOR ALL USING (
    id IN (
        SELECT wm.workspace_id
        FROM workspace_members wm
        WHERE wm.user_id = current_app_user_id()
    )
    AND is_demo = COALESCE(
        (SELECT up.demo_mode_enabled FROM user_preferences up WHERE up.user_id = current_app_user_id()),
        FALSE
    )
);

DROP POLICY IF EXISTS members_isolation ON workspace_members;
CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
    user_id = current_app_user_id()
) WITH CHECK (
    user_id = current_app_user_id()
);

DROP POLICY IF EXISTS workspace_owners_isolation ON workspace_owners;
DROP TABLE IF EXISTS workspace_owners;

DROP INDEX IF EXISTS idx_unique_owner_per_user;
ALTER TABLE workspaces DROP COLUMN IF EXISTS owner_user_id;

DROP FUNCTION IF EXISTS current_app_invite_token_hash();
DROP FUNCTION IF EXISTS current_app_workspace_id();
