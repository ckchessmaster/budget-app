DROP POLICY IF EXISTS workspace_isolation ON workspaces;

CREATE POLICY workspace_isolation ON workspaces FOR ALL USING (
    id IN (SELECT current_app_user_workspace_ids())
);
