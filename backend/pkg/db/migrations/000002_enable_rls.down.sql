DROP POLICY IF EXISTS simplefin_isolation ON simplefin_connections;
DROP POLICY IF EXISTS receipts_isolation ON receipts;
DROP POLICY IF EXISTS transactions_isolation ON transactions;
DROP POLICY IF EXISTS reallocations_isolation ON budget_reallocations;
DROP POLICY IF EXISTS incomes_isolation ON monthly_planned_incomes;
DROP POLICY IF EXISTS budgets_isolation ON monthly_category_budgets;
DROP POLICY IF EXISTS categories_isolation ON categories;
DROP POLICY IF EXISTS accounts_isolation ON accounts;
DROP POLICY IF EXISTS settings_isolation ON workspace_settings;
DROP POLICY IF EXISTS members_isolation ON workspace_members;
DROP POLICY IF EXISTS workspace_isolation ON workspaces;

DROP FUNCTION IF EXISTS current_app_user_workspace_ids();
DROP FUNCTION IF EXISTS current_app_user_id();

ALTER TABLE simplefin_connections NO FORCE ROW LEVEL SECURITY;
ALTER TABLE receipts NO FORCE ROW LEVEL SECURITY;
ALTER TABLE transactions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE budget_reallocations NO FORCE ROW LEVEL SECURITY;
ALTER TABLE monthly_planned_incomes NO FORCE ROW LEVEL SECURITY;
ALTER TABLE monthly_category_budgets NO FORCE ROW LEVEL SECURITY;
ALTER TABLE categories NO FORCE ROW LEVEL SECURITY;
ALTER TABLE accounts NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_settings NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_members NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workspaces NO FORCE ROW LEVEL SECURITY;

ALTER TABLE simplefin_connections DISABLE ROW LEVEL SECURITY;
ALTER TABLE receipts DISABLE ROW LEVEL SECURITY;
ALTER TABLE transactions DISABLE ROW LEVEL SECURITY;
ALTER TABLE budget_reallocations DISABLE ROW LEVEL SECURITY;
ALTER TABLE monthly_planned_incomes DISABLE ROW LEVEL SECURITY;
ALTER TABLE monthly_category_budgets DISABLE ROW LEVEL SECURITY;
ALTER TABLE categories DISABLE ROW LEVEL SECURITY;
ALTER TABLE accounts DISABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_settings DISABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_members DISABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces DISABLE ROW LEVEL SECURITY;
