ALTER TABLE push_subscriptions
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user_id
    ON push_subscriptions (user_id);

ALTER TABLE push_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE push_subscriptions FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS push_subscriptions_isolation ON push_subscriptions;
CREATE POLICY push_subscriptions_isolation ON push_subscriptions FOR ALL USING (
    user_id = current_app_user_id()
) WITH CHECK (
    user_id = current_app_user_id()
);

CREATE TABLE IF NOT EXISTS notification_cooldowns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    trigger_type VARCHAR(64) NOT NULL CHECK (trigger_type IN ('SPENDING_VELOCITY', 'STATEMENT_DUE')),
    entity_id UUID NOT NULL,
    last_sent_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, trigger_type, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_notification_cooldowns_ws_trigger
    ON notification_cooldowns (workspace_id, trigger_type, last_sent_at DESC);

ALTER TABLE notification_cooldowns ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_cooldowns FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS notification_cooldowns_isolation ON notification_cooldowns;
CREATE POLICY notification_cooldowns_isolation ON notification_cooldowns FOR ALL USING (
    workspace_id IN (SELECT id FROM workspaces)
) WITH CHECK (
    workspace_id IN (SELECT id FROM workspaces)
);
