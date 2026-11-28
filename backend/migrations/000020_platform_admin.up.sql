-- Platform Admin Interface & Invite-Only Friend Onboarding

CREATE TYPE platform_invite_status AS ENUM ('ACTIVE', 'REDEEMED', 'REVOKED', 'EXPIRED');

-- Platform-level user registry (synced on OIDC login / invite redemption)
CREATE TABLE platform_users (
    user_id UUID PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Backfill existing users from workspace_roster and workspace_members
WITH ranked_roster AS (
    SELECT DISTINCT ON (user_id)
        user_id,
        LOWER(TRIM(email)) AS raw_email,
        TRIM(display_name) AS display_name,
        created_at
    FROM workspace_roster
    ORDER BY user_id, (NULLIF(TRIM(email), '') IS NULL) ASC, created_at ASC
),
dedup_emails AS (
    SELECT
        user_id,
        raw_email,
        display_name,
        created_at,
        ROW_NUMBER() OVER (PARTITION BY raw_email ORDER BY created_at ASC, user_id ASC) AS email_rn
    FROM ranked_roster
)
INSERT INTO platform_users (user_id, email, display_name, is_active, created_at)
SELECT
    user_id,
    CASE
        WHEN raw_email <> '' AND email_rn = 1 THEN raw_email
        ELSE user_id::text || '@unknown.local'
    END,
    display_name,
    TRUE,
    created_at
FROM dedup_emails
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO platform_users (user_id, email, display_name, is_active)
SELECT DISTINCT
    wm.user_id,
    wm.user_id::text || '@unknown.local',
    '',
    TRUE
FROM workspace_members wm
WHERE NOT EXISTS (SELECT 1 FROM platform_users pu WHERE pu.user_id = wm.user_id)
ON CONFLICT DO NOTHING;

-- Open single-use platform invite tokens
CREATE TABLE platform_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label VARCHAR(120) NOT NULL DEFAULT '',
    recipient_email VARCHAR(255) NOT NULL DEFAULT '',
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    encrypted_pending_token TEXT,
    status platform_invite_status NOT NULL DEFAULT 'ACTIVE',
    created_by_user_id UUID REFERENCES platform_users(user_id) ON DELETE SET NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    redeemed_by_user_id UUID REFERENCES platform_users(user_id) ON DELETE SET NULL,
    redeemed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_platform_invitations_status ON platform_invitations(status, expires_at);

-- Track SimpleFIN sync health metadata without exposing financial tables
ALTER TABLE simplefin_connections
    ADD COLUMN IF NOT EXISTS last_sync_status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    ADD COLUMN IF NOT EXISTS last_sync_error TEXT;

-- Outbound email audit log for operational health telemetry
CREATE TABLE outbound_email_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_outbound_email_logs_created ON outbound_email_logs(created_at DESC);

-- GUC helpers for platform admin metadata views/functions and public invite token lookup
CREATE OR REPLACE FUNCTION is_platform_admin_mode() RETURNS BOOLEAN AS $$
    SELECT COALESCE(current_setting('app.platform_admin_mode', true), '') = 'true';
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION current_app_platform_invite_token_hash() RETURNS TEXT AS $$
    SELECT NULLIF(current_setting('app.platform_invite_token_hash', true), '');
$$ LANGUAGE SQL STABLE;

ALTER TABLE platform_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_invitations FORCE ROW LEVEL SECURITY;

CREATE POLICY platform_invitations_isolation ON platform_invitations FOR ALL USING (
    is_platform_admin_mode()
    OR (
        current_app_platform_invite_token_hash() IS NOT NULL
        AND token_hash = current_app_platform_invite_token_hash()
    )
) WITH CHECK (
    is_platform_admin_mode()
    OR (
        current_app_platform_invite_token_hash() IS NOT NULL
        AND token_hash = current_app_platform_invite_token_hash()
    )
);

-- Privacy-enforcing metadata view for Platform Admins.
-- Executes with view-owner privileges (security_invoker = false) so it can summarize
-- non-demo workspace metadata without loosening tenant RLS on financial ledger tables.
CREATE VIEW admin_workspace_overview AS
SELECT
    w.id AS workspace_id,
    w.name AS workspace_name,
    w.created_at,
    COUNT(wm.user_id)::INT AS member_count,
    MAX(CASE WHEN wm.role = 'owner' THEN pu.email END) AS owner_email,
    sc.last_synced_at AS simplefin_last_synced_at,
    COALESCE(sc.last_sync_status, 'NOT_CONFIGURED') AS simplefin_status,
    sc.last_sync_error AS simplefin_error
FROM workspaces w
LEFT JOIN workspace_members wm ON wm.workspace_id = w.id
LEFT JOIN platform_users pu ON pu.user_id = wm.user_id
LEFT JOIN simplefin_connections sc ON sc.workspace_id = w.id
WHERE is_platform_admin_mode()
  AND w.is_demo = FALSE
GROUP BY w.id, w.name, w.created_at, sc.last_synced_at, sc.last_sync_status, sc.last_sync_error;

-- Security-definer admin helper: lists platform users with owned/member non-demo workspace counts.
CREATE OR REPLACE FUNCTION admin_list_users()
RETURNS TABLE (
    user_id UUID,
    email VARCHAR(255),
    display_name VARCHAR(255),
    is_active BOOLEAN,
    last_login_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE,
    owned_workspace_count INT,
    member_workspace_count INT
) AS $$
BEGIN
    IF NOT is_platform_admin_mode() THEN
        RAISE EXCEPTION 'platform_admin_mode_required';
    END IF;

    RETURN QUERY
    SELECT
        pu.user_id,
        pu.email,
        pu.display_name,
        pu.is_active,
        pu.last_login_at,
        pu.created_at,
        COALESCE(COUNT(DISTINCT CASE WHEN w.id IS NOT NULL AND wm.role = 'owner' THEN w.id END), 0)::INT AS owned_workspace_count,
        COALESCE(COUNT(DISTINCT CASE WHEN w.id IS NOT NULL AND wm.role <> 'owner' THEN w.id END), 0)::INT AS member_workspace_count
    FROM platform_users pu
    LEFT JOIN workspace_members wm ON wm.user_id = pu.user_id
    LEFT JOIN workspaces w ON w.id = wm.workspace_id AND w.is_demo = FALSE
    GROUP BY pu.user_id, pu.email, pu.display_name, pu.is_active, pu.last_login_at, pu.created_at
    ORDER BY pu.created_at DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

-- Security-definer admin helper: deletes a user, cascades their owned workspaces (including demo),
-- removes them from shared workspaces, and deletes their platform_users record.
CREATE OR REPLACE FUNCTION admin_delete_user(target_user_id UUID) RETURNS BOOLEAN AS $$
DECLARE
    deleted_count INT;
BEGIN
    IF NOT is_platform_admin_mode() THEN
        RAISE EXCEPTION 'platform_admin_mode_required';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM platform_users WHERE user_id = target_user_id) THEN
        RETURN FALSE;
    END IF;

    -- Delete any workspace owned by target_user_id (ON DELETE CASCADE removes all financial records)
    DELETE FROM workspaces w
    WHERE w.owner_user_id = target_user_id
       OR EXISTS (
           SELECT 1
           FROM workspace_members wm
           WHERE wm.workspace_id = w.id
             AND wm.user_id = target_user_id
             AND wm.role = 'owner'
       );

    -- Remove target_user_id from any remaining shared workspaces
    DELETE FROM workspace_roster WHERE user_id = target_user_id;
    DELETE FROM workspace_members WHERE user_id = target_user_id;
    DELETE FROM push_subscriptions WHERE user_id = target_user_id;
    DELETE FROM user_preferences WHERE user_id = target_user_id;

    DELETE FROM platform_users WHERE user_id = target_user_id;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;

    RETURN deleted_count > 0;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'budget_app_user') THEN
        GRANT ALL PRIVILEGES ON platform_users, platform_invitations, outbound_email_logs, admin_workspace_overview TO budget_app_user;
        GRANT EXECUTE ON FUNCTION is_platform_admin_mode(), current_app_platform_invite_token_hash(), admin_list_users(), admin_delete_user(UUID) TO budget_app_user;
    END IF;
END
$$;
