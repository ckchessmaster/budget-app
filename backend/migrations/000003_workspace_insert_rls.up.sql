-- Allow authenticated users to create a new workspace
CREATE POLICY workspace_insert ON workspaces FOR INSERT WITH CHECK (
    current_app_user_id() IS NOT NULL
);

-- Replace members_isolation to avoid self-recursion via current_app_user_workspace_ids(),
-- and allow users to insert their own membership row when provisioning a workspace.
DROP POLICY IF EXISTS members_isolation ON workspace_members;
CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
    user_id = current_app_user_id()
) WITH CHECK (
    user_id = current_app_user_id()
);
