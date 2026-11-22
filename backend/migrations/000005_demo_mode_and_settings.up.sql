ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS is_demo BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS settled_delta NUMERIC(12, 2);

ALTER TABLE simplefin_connections
    ADD COLUMN IF NOT EXISTS last_sync_warnings JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id UUID PRIMARY KEY,
    demo_mode_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

ALTER TABLE user_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_preferences FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS user_preferences_isolation ON user_preferences;
CREATE POLICY user_preferences_isolation ON user_preferences FOR ALL USING (
    user_id = current_app_user_id()
) WITH CHECK (
    user_id = current_app_user_id()
);

CREATE OR REPLACE FUNCTION current_app_user_workspace_ids() RETURNS SETOF UUID AS $$
    SELECT wm.workspace_id
    FROM workspace_members wm
    JOIN workspaces w ON w.id = wm.workspace_id
    WHERE wm.user_id = current_app_user_id()
      AND w.is_demo = COALESCE(
          (SELECT up.demo_mode_enabled FROM user_preferences up WHERE up.user_id = current_app_user_id()),
          FALSE
      );
$$ LANGUAGE SQL STABLE SECURITY DEFINER;

CREATE TABLE IF NOT EXISTS demo_simplefin_state (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    auth_username VARCHAR(100) NOT NULL,
    auth_password VARCHAR(100) NOT NULL,
    fault_mode VARCHAR(32) NOT NULL DEFAULT 'NONE',
    fault_persistent BOOLEAN NOT NULL DEFAULT FALSE,
    fault_retry_after_seconds INT NOT NULL DEFAULT 3,
    fault_message TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS demo_simplefin_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    external_account_id VARCHAR(255) NOT NULL,
    name VARCHAR(150) NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'USD',
    balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    account_type account_type NOT NULL,
    card_mode cc_mode NOT NULL DEFAULT 'PAY_IN_FULL',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, external_account_id)
);

CREATE TABLE IF NOT EXISTS demo_simplefin_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    external_account_id VARCHAR(255) NOT NULL,
    external_transaction_id VARCHAR(255) NOT NULL,
    posted_at TIMESTAMP WITH TIME ZONE NOT NULL,
    amount NUMERIC(12, 2) NOT NULL,
    description VARCHAR(255) NOT NULL,
    pending BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, external_transaction_id)
);

CREATE INDEX IF NOT EXISTS idx_demo_sf_tx_workspace_posted
    ON demo_simplefin_transactions (workspace_id, posted_at DESC);

ALTER TABLE demo_simplefin_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_simplefin_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE demo_simplefin_transactions ENABLE ROW LEVEL SECURITY;

ALTER TABLE demo_simplefin_state FORCE ROW LEVEL SECURITY;
ALTER TABLE demo_simplefin_accounts FORCE ROW LEVEL SECURITY;
ALTER TABLE demo_simplefin_transactions FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS demo_sf_state_isolation ON demo_simplefin_state;
CREATE POLICY demo_sf_state_isolation ON demo_simplefin_state FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS demo_sf_accounts_isolation ON demo_simplefin_accounts;
CREATE POLICY demo_sf_accounts_isolation ON demo_simplefin_accounts FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);

DROP POLICY IF EXISTS demo_sf_transactions_isolation ON demo_simplefin_transactions;
CREATE POLICY demo_sf_transactions_isolation ON demo_simplefin_transactions FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
);
