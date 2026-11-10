ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE monthly_category_budgets ENABLE ROW LEVEL SECURITY;
ALTER TABLE monthly_planned_incomes ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_reallocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE simplefin_connections ENABLE ROW LEVEL SECURITY;

ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_members FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_settings FORCE ROW LEVEL SECURITY;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE categories FORCE ROW LEVEL SECURITY;
ALTER TABLE monthly_category_budgets FORCE ROW LEVEL SECURITY;
ALTER TABLE monthly_planned_incomes FORCE ROW LEVEL SECURITY;
ALTER TABLE budget_reallocations FORCE ROW LEVEL SECURITY;
ALTER TABLE transactions FORCE ROW LEVEL SECURITY;
ALTER TABLE receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE simplefin_connections FORCE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION current_app_user_id() RETURNS UUID AS $$
    SELECT NULLIF(current_setting('app.current_user_id', true), '')::UUID;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION current_app_user_workspace_ids() RETURNS SETOF UUID AS $$
    SELECT workspace_id FROM workspace_members WHERE user_id = current_app_user_id();
$$ LANGUAGE SQL STABLE SECURITY DEFINER;

CREATE POLICY workspace_isolation ON workspaces FOR ALL USING (
    id IN (SELECT current_app_user_workspace_ids())
);
CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
    workspace_id IN (SELECT current_app_user_workspace_ids())
);
CREATE POLICY settings_isolation ON workspace_settings FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY accounts_isolation ON accounts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY categories_isolation ON categories FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY budgets_isolation ON monthly_category_budgets FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY incomes_isolation ON monthly_planned_incomes FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY reallocations_isolation ON budget_reallocations FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY transactions_isolation ON transactions FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY receipts_isolation ON receipts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
CREATE POLICY simplefin_isolation ON simplefin_connections FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'budget_app_user') THEN
    CREATE ROLE budget_app_user WITH LOGIN PASSWORD 'budgetpass';
  END IF;
END
$$;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO budget_app_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO budget_app_user;
GRANT ALL PRIVILEGES ON SCHEMA public TO budget_app_user;
