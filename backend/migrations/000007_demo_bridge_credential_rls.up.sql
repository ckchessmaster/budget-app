-- Make the demo SimpleFIN bridge readable.
--
-- demo_simplefin_state, demo_simplefin_accounts and demo_simplefin_transactions are all
-- FORCE ROW LEVEL SECURITY and isolated through
--     workspace_id IN (SELECT id FROM workspaces)
-- which resolves to zero rows unless the session carries app.current_user_id. Every
-- other reader of these tables runs inside db.BeginWorkspaceTx, which sets it. The bridge
-- cannot: it authenticates with the per-workspace credentials that SimpleFIN puts in the
-- access URL (demo.buildMockAccessURL) and has no OIDC subject to resolve. Its store
-- opens a bare transaction, so the credential lookup found no row and the bridge answered
-- 404 for a perfectly correct access URL -- which is why demo bank sync never imported
-- anything.
--
-- The bridge therefore needs an authorisation path expressed in terms of what it
-- actually presents. These policies gain a second, credential-matched arm alongside the
-- existing user-scoped one: the caller supplies the workspace id plus both halves of the
-- credential pair through transaction-local settings, and rows are visible only when that
-- triple matches the state row. Nothing about the user-scoped arm changes, so the API
-- keeps enforcing workspace visibility and the demo_mode_enabled preference exactly as
-- before.
--
-- Deliberately not done here, and why:
--   * No SECURITY DEFINER function bypasses RLS on these tables. Every one of them is
--     FORCE ROW LEVEL SECURITY and the migration role is not a superuser in production,
--     so a definer function runs into the very policy it is trying to read around. That
--     is the trap migration 000006 had to undo; the helpers below read only configuration
--     settings, never a table, so no policy reference can re-enter itself.
--   * No user_id column backfilled by this migration. An UPDATE here would itself be
--     subject to the same RLS and would silently update zero rows in production while
--     passing in CI, where migrations run as a superuser. The bridge fix must work for
--     rows that already exist.

-- Helpers read only GUCs. No table access means no possibility of policy recursion.--
-- The workspace id stays TEXT on purpose. Casting the setting to UUID would raise on a
-- malformed value, and because a policy applies to every statement against these tables,
-- one bad setting would then error out ordinary tenant reads as well. Comparing as text
-- makes a malformed or unexpected value simply fail to match, which denies instead of
-- breaking. The bridge always supplies uuid.String(), already validated by chi parsing the
-- path parameter, so the comparison is exact in practice.
CREATE OR REPLACE FUNCTION current_demo_bridge_workspace_id() RETURNS TEXT AS $$
    SELECT NULLIF(current_setting('app.demo_bridge_workspace_id', true), '');
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION current_demo_bridge_username() RETURNS TEXT AS $$
    SELECT NULLIF(current_setting('app.demo_bridge_username', true), '');
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION current_demo_bridge_password() RETURNS TEXT AS $$
    SELECT NULLIF(current_setting('app.demo_bridge_password', true), '');
$$ LANGUAGE SQL STABLE;

-- The state row holds the credentials, so it is the one row the credential arm must gate
-- on itself rather than through a subquery. All three guards are required: without them an
-- unset setting would compare NULL and simply fail to match, which is the intent, but the
-- explicit IS NOT NULL checks keep the arm reading as a handshake rather than as arithmetic
-- on missing input.
DROP POLICY IF EXISTS demo_sf_state_isolation ON demo_simplefin_state;

CREATE POLICY demo_sf_state_isolation ON demo_simplefin_state FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
    OR (
        workspace_id::TEXT = current_demo_bridge_workspace_id()
        AND current_demo_bridge_username() IS NOT NULL
        AND current_demo_bridge_password() IS NOT NULL
        AND auth_username = current_demo_bridge_username()
        AND auth_password = current_demo_bridge_password()
    )
);

-- Accounts and transactions reuse that credential check instead of repeating it, so a
-- session that guessed a workspace id but not its credentials cannot read either table.
-- Scanning demo_simplefin_state here is safe: its own policy consults only workspaces and
-- the settings above, so the chain terminates instead of re-entering.
DROP POLICY IF EXISTS demo_sf_accounts_isolation ON demo_simplefin_accounts;

CREATE POLICY demo_sf_accounts_isolation ON demo_simplefin_accounts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
    OR (
        workspace_id::TEXT = current_demo_bridge_workspace_id()
        AND EXISTS (
            SELECT 1
            FROM demo_simplefin_state s
            WHERE s.workspace_id = demo_simplefin_accounts.workspace_id
              AND s.auth_username = current_demo_bridge_username()
              AND s.auth_password = current_demo_bridge_password()
        )
    )
);

DROP POLICY IF EXISTS demo_sf_transactions_isolation ON demo_simplefin_transactions;

CREATE POLICY demo_sf_transactions_isolation ON demo_simplefin_transactions FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
    OR (
        workspace_id::TEXT = current_demo_bridge_workspace_id()
        AND EXISTS (
            SELECT 1
            FROM demo_simplefin_state s
            WHERE s.workspace_id = demo_simplefin_transactions.workspace_id
              AND s.auth_username = current_demo_bridge_username()
              AND s.auth_password = current_demo_bridge_password()
        )
    )
);
