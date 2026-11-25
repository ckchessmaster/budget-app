DROP POLICY IF EXISTS notification_cooldowns_isolation ON notification_cooldowns;
DROP TABLE IF EXISTS notification_cooldowns;

DROP POLICY IF EXISTS push_subscriptions_isolation ON push_subscriptions;
ALTER TABLE push_subscriptions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE push_subscriptions DISABLE ROW LEVEL SECURITY;
DROP INDEX IF EXISTS idx_push_subscriptions_user_id;
ALTER TABLE push_subscriptions
    DROP COLUMN IF EXISTS updated_at;
