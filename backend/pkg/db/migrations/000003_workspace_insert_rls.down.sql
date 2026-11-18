DROP POLICY IF EXISTS workspace_insert ON workspaces;
DROP POLICY IF EXISTS members_isolation ON workspace_members;
CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
    workspace_id IN (SELECT current_app_user_workspace_ids())
);
