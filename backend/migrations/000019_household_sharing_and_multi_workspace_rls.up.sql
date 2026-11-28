-- Household Sharing, Multi-Workspace Switching & Family Invites

CREATE TYPE invite_status AS ENUM ('PENDING', 'ACCEPTED', 'REVOKED', 'EXPIRED');

CREATE TABLE workspace_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    workspace_name VARCHAR(255) NOT NULL DEFAULT '',
    invited_by_user_id UUID NOT NULL,
    invited_by_email VARCHAR(255) NOT NULL DEFAULT '',
    email VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'read_only')),
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    encrypted_pending_token TEXT,
    status invite_status NOT NULL DEFAULT 'PENDING',
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    accepted_by_user_id UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Enforce at most one active pending invite per email per workspace
CREATE UNIQUE INDEX idx_unique_pending_invite
    ON workspace_invitations (workspace_id, LOWER(email))
    WHERE status = 'PENDING';

-- Enforce at most one non-demo self-created/owned workspace per user
-- (Demo sandboxes are excluded so users can toggle Demo Mode alongside their owned workspace)
ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS owner_user_id UUID;

CREATE UNIQUE INDEX idx_unique_owner_per_user
    ON workspaces (owner_user_id)
    WHERE owner_user_id IS NOT NULL AND is_demo = FALSE;

-- Non-recursive owner lookup table so workspace_members RLS can grant owners access
-- to manage members without querying workspaces or workspace_members itself.
CREATE TABLE workspace_owners (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL
);

ALTER TABLE workspace_owners ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_owners FORCE ROW LEVEL SECURITY;

CREATE POLICY workspace_owners_isolation ON workspace_owners FOR ALL USING (
    owner_user_id = current_app_user_id()
) WITH CHECK (
    owner_user_id = current_app_user_id()
);

-- Member directory visible to all members of the active workspace
CREATE TABLE workspace_roster (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    role VARCHAR(50) NOT NULL CHECK (role IN ('owner', 'member', 'read_only')),
    email VARCHAR(255) NOT NULL DEFAULT '',
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, user_id)
);

-- Crash-proof GUC helpers (never raise on malformed settings)
CREATE OR REPLACE FUNCTION current_app_workspace_id() RETURNS UUID AS $$
    SELECT CASE
        WHEN NULLIF(current_setting('app.current_workspace_id', true), '') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
        THEN current_setting('app.current_workspace_id', true)::UUID
        ELSE NULL
    END;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION current_app_invite_token_hash() RETURNS TEXT AS $$
    SELECT NULLIF(current_setting('app.invite_token_hash', true), '');
$$ LANGUAGE SQL STABLE;

-- Updated non-recursive RLS chain:
--   child tables / workspace_roster -> workspaces -> workspace_members -> workspace_owners -> GUC
DROP POLICY IF EXISTS members_isolation ON workspace_members;
CREATE POLICY members_isolation ON workspace_members FOR ALL USING (
    user_id = current_app_user_id()
    OR workspace_id IN (
        SELECT wo.workspace_id
        FROM workspace_owners wo
        WHERE wo.owner_user_id = current_app_user_id()
    )
) WITH CHECK (
    user_id = current_app_user_id()
    OR workspace_id IN (
        SELECT wo.workspace_id
        FROM workspace_owners wo
        WHERE wo.owner_user_id = current_app_user_id()
    )
);

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
    AND (current_app_workspace_id() IS NULL OR id = current_app_workspace_id())
);

ALTER TABLE workspace_roster ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_roster FORCE ROW LEVEL SECURITY;

CREATE POLICY workspace_roster_isolation ON workspace_roster FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
) WITH CHECK (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

ALTER TABLE workspace_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_invitations FORCE ROW LEVEL SECURITY;

CREATE POLICY invitations_workspace_isolation ON workspace_invitations FOR ALL USING (
    (
        (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
        AND workspace_id IN (SELECT id FROM workspaces)
    )
    OR (
        current_app_invite_token_hash() IS NOT NULL
        AND token_hash = current_app_invite_token_hash()
    )
) WITH CHECK (
    (
        (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
        AND workspace_id IN (SELECT id FROM workspaces)
    )
    OR (
        current_app_invite_token_hash() IS NOT NULL
        AND token_hash = current_app_invite_token_hash()
    )
);

-- Enforce active workspace context across all workspace-scoped tables
DROP POLICY IF EXISTS settings_isolation ON workspace_settings;
CREATE POLICY settings_isolation ON workspace_settings FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS accounts_isolation ON accounts;
CREATE POLICY accounts_isolation ON accounts FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS categories_isolation ON categories;
CREATE POLICY categories_isolation ON categories FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS budgets_isolation ON monthly_category_budgets;
CREATE POLICY budgets_isolation ON monthly_category_budgets FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS incomes_isolation ON monthly_planned_incomes;
CREATE POLICY incomes_isolation ON monthly_planned_incomes FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS reallocations_isolation ON budget_reallocations;
CREATE POLICY reallocations_isolation ON budget_reallocations FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS transactions_isolation ON transactions;
CREATE POLICY transactions_isolation ON transactions FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS receipts_isolation ON receipts;
CREATE POLICY receipts_isolation ON receipts FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS simplefin_isolation ON simplefin_connections;
CREATE POLICY simplefin_isolation ON simplefin_connections FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS month_end_rollover_runs_isolation ON month_end_rollover_runs;
CREATE POLICY month_end_rollover_runs_isolation ON month_end_rollover_runs FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS cc_debt_interlocks_isolation ON credit_card_debt_interlocks;
CREATE POLICY cc_debt_interlocks_isolation ON credit_card_debt_interlocks FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments;
CREATE POLICY monthly_rollover_adjustments_isolation ON monthly_rollover_adjustments FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS notification_cooldowns_isolation ON notification_cooldowns;
CREATE POLICY notification_cooldowns_isolation ON notification_cooldowns FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
) WITH CHECK (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS workspace_isolation_categorization_rules ON categorization_rules;
CREATE POLICY workspace_isolation_categorization_rules ON categorization_rules FOR ALL USING (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
) WITH CHECK (
    (current_app_workspace_id() IS NULL OR workspace_id = current_app_workspace_id())
    AND workspace_id IN (SELECT id FROM workspaces)
);
