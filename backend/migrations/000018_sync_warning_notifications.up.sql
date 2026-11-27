ALTER TABLE notification_cooldowns
    DROP CONSTRAINT IF EXISTS notification_cooldowns_trigger_type_check;

ALTER TABLE notification_cooldowns
    ADD CONSTRAINT notification_cooldowns_trigger_type_check
    CHECK (trigger_type IN ('SPENDING_VELOCITY', 'STATEMENT_DUE', 'SYNC_WARNING'));
