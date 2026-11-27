ALTER TABLE categories
    ADD COLUMN budget_alert_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN budget_alert_threshold_pct INTEGER NOT NULL DEFAULT 80
        CHECK (budget_alert_threshold_pct >= 1 AND budget_alert_threshold_pct <= 200);
