ALTER TABLE categories
    DROP COLUMN IF EXISTS budget_alert_threshold_pct,
    DROP COLUMN IF EXISTS budget_alert_enabled;
