-- Fix unbounded RLS recursion introduced by migration 000005.
--
-- Migration 000005 rewrote current_app_user_workspace_ids() to JOIN workspaces so it
-- could filter by is_demo, while workspace_isolation kept its USING clause of
--     id IN (SELECT current_app_user_workspace_ids())
-- Because workspaces is FORCE ROW LEVEL SECURITY and the SECURITY DEFINER function is
-- owned by the (non-superuser) role that runs migrations, the function's own scan of
-- workspaces re-applies that policy, which calls the function again. Every scan of
-- every tenant table subqueries workspaces, so all API reads aborted with
-- "stack depth limit exceeded".
--
-- Express the same predicate inline instead. is_demo is a column of the row being
-- filtered, and the workspace_members and user_preferences policies only read
-- current_app_user_id(), so no policy reference re-enters workspaces.
-- Visibility semantics are unchanged: a user sees the workspaces they belong to whose
-- is_demo matches their demo_mode_enabled preference.
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

-- current_app_user_workspace_ids() is no longer referenced by any policy. Keep it
-- available to ad-hoc callers, but it must never be used in a workspaces policy again.
