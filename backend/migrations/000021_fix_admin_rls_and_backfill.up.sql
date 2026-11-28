-- Fix admin metadata visibility and user backfill when migrations/backend run as a
-- non-superuser table owner subject to FORCE ROW LEVEL SECURITY.

CREATE TABLE IF NOT EXISTS platform_workspace_metadata (
    workspace_id UUID PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    workspace_name VARCHAR(255) NOT NULL,
    is_demo BOOLEAN NOT NULL DEFAULT FALSE,
    owner_user_id UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    simplefin_configured BOOLEAN NOT NULL DEFAULT FALSE,
    simplefin_last_synced_at TIMESTAMP WITH TIME ZONE,
    simplefin_last_sync_status VARCHAR(32) NOT NULL DEFAULT 'NOT_CONFIGURED',
    simplefin_last_sync_error TEXT
);

CREATE TABLE IF NOT EXISTS platform_workspace_members (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    role VARCHAR(50) NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
);

ALTER TABLE platform_workspace_metadata ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_workspace_metadata FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS platform_workspace_metadata_isolation ON platform_workspace_metadata;
CREATE POLICY platform_workspace_metadata_isolation ON platform_workspace_metadata FOR ALL USING (
    is_platform_admin_mode()
) WITH CHECK (
    is_platform_admin_mode()
);

ALTER TABLE platform_workspace_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform_workspace_members FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS platform_workspace_members_isolation ON platform_workspace_members;
CREATE POLICY platform_workspace_members_isolation ON platform_workspace_members FOR ALL USING (
    is_platform_admin_mode()
) WITH CHECK (
    is_platform_admin_mode()
);

-- Temporarily lift FORCE RLS (which otherwise applies even to a non-superuser table owner)
-- inside this migration transaction to backfill existing users, workspaces, memberships,
-- and SimpleFIN metadata, then immediately restore FORCE ROW LEVEL SECURITY.
ALTER TABLE workspaces NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_members NO FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_roster NO FORCE ROW LEVEL SECURITY;
ALTER TABLE simplefin_connections NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform_workspace_metadata NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform_workspace_members NO FORCE ROW LEVEL SECURITY;

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
    d.user_id,
    CASE
        WHEN d.raw_email <> ''
         AND d.email_rn = 1
         AND NOT EXISTS (
             SELECT 1 FROM platform_users pu2
             WHERE LOWER(pu2.email) = d.raw_email AND pu2.user_id <> d.user_id
         )
        THEN d.raw_email
        ELSE d.user_id::text || '@unknown.local'
    END,
    d.display_name,
    TRUE,
    d.created_at
FROM dedup_emails d
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

INSERT INTO platform_workspace_metadata (
    workspace_id,
    workspace_name,
    is_demo,
    owner_user_id,
    created_at,
    simplefin_configured,
    simplefin_last_synced_at,
    simplefin_last_sync_status,
    simplefin_last_sync_error
)
SELECT
    w.id,
    w.name,
    w.is_demo,
    w.owner_user_id,
    COALESCE(w.created_at, NOW()),
    (sc.workspace_id IS NOT NULL),
    sc.last_synced_at,
    COALESCE(sc.last_sync_status, 'NOT_CONFIGURED'),
    sc.last_sync_error
FROM workspaces w
LEFT JOIN simplefin_connections sc ON sc.workspace_id = w.id
ON CONFLICT (workspace_id) DO UPDATE
SET workspace_name = EXCLUDED.workspace_name,
    is_demo = EXCLUDED.is_demo,
    owner_user_id = EXCLUDED.owner_user_id,
    simplefin_configured = EXCLUDED.simplefin_configured,
    simplefin_last_synced_at = EXCLUDED.simplefin_last_synced_at,
    simplefin_last_sync_status = EXCLUDED.simplefin_last_sync_status,
    simplefin_last_sync_error = EXCLUDED.simplefin_last_sync_error;

INSERT INTO platform_workspace_members (workspace_id, user_id, role)
SELECT wm.workspace_id, wm.user_id, wm.role
FROM workspace_members wm
ON CONFLICT (workspace_id, user_id) DO UPDATE
SET role = EXCLUDED.role;

ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_members FORCE ROW LEVEL SECURITY;
ALTER TABLE workspace_roster FORCE ROW LEVEL SECURITY;
ALTER TABLE simplefin_connections FORCE ROW LEVEL SECURITY;
ALTER TABLE platform_workspace_metadata FORCE ROW LEVEL SECURITY;
ALTER TABLE platform_workspace_members FORCE ROW LEVEL SECURITY;

-- Triggers to keep metadata tables synchronized without exposing tenant ledger tables
CREATE OR REPLACE FUNCTION sync_platform_workspace_metadata_from_workspace() RETURNS TRIGGER AS $$
DECLARE
    prev_mode TEXT;
BEGIN
    prev_mode := current_setting('app.platform_admin_mode', true);
    BEGIN
        PERFORM set_config('app.platform_admin_mode', 'true', true);
        INSERT INTO platform_workspace_metadata (
            workspace_id, workspace_name, is_demo, owner_user_id, created_at
        )
        VALUES (
            NEW.id, NEW.name, NEW.is_demo, NEW.owner_user_id, COALESCE(NEW.created_at, NOW())
        )
        ON CONFLICT (workspace_id) DO UPDATE
        SET workspace_name = EXCLUDED.workspace_name,
            is_demo = EXCLUDED.is_demo,
            owner_user_id = EXCLUDED.owner_user_id;
        PERFORM set_config('app.platform_admin_mode', COALESCE(prev_mode, ''), true);
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('app.platform_admin_mode', COALESCE(prev_mode, ''), true);
        RAISE;
    END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

DROP TRIGGER IF EXISTS trg_sync_platform_workspace_metadata ON workspaces;
CREATE TRIGGER trg_sync_platform_workspace_metadata
AFTER INSERT OR UPDATE ON workspaces
FOR EACH ROW EXECUTE FUNCTION sync_platform_workspace_metadata_from_workspace();

CREATE OR REPLACE FUNCTION sync_platform_workspace_members() RETURNS TRIGGER AS $$
DECLARE
    prev_mode TEXT;
BEGIN
    prev_mode := current_setting('app.platform_admin_mode', true);
    BEGIN
        PERFORM set_config('app.platform_admin_mode', 'true', true);
        IF TG_OP = 'DELETE' THEN
            DELETE FROM platform_workspace_members
            WHERE workspace_id = OLD.workspace_id AND user_id = OLD.user_id;
        ELSE
            INSERT INTO platform_workspace_members (workspace_id, user_id, role)
            VALUES (NEW.workspace_id, NEW.user_id, NEW.role)
            ON CONFLICT (workspace_id, user_id) DO UPDATE
            SET role = EXCLUDED.role;
        END IF;
        PERFORM set_config('app.platform_admin_mode', COALESCE(prev_mode, ''), true);
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('app.platform_admin_mode', COALESCE(prev_mode, ''), true);
        RAISE;
    END;
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

DROP TRIGGER IF EXISTS trg_sync_platform_workspace_members ON workspace_members;
CREATE TRIGGER trg_sync_platform_workspace_members
AFTER INSERT OR UPDATE OR DELETE ON workspace_members
FOR EACH ROW EXECUTE FUNCTION sync_platform_workspace_members();

CREATE OR REPLACE FUNCTION sync_platform_workspace_simplefin() RETURNS TRIGGER AS $$
DECLARE
    prev_mode TEXT;
BEGIN
    prev_mode := current_setting('app.platform_admin_mode', true);
    BEGIN
        PERFORM set_config('app.platform_admin_mode', 'true', true);
        IF TG_OP = 'DELETE' THEN
            UPDATE platform_workspace_metadata
            SET simplefin_configured = FALSE,
                simplefin_last_synced_at = NULL,
                simplefin_last_sync_status = 'NOT_CONFIGURED',
                simplefin_last_sync_error = NULL
            WHERE workspace_id = OLD.workspace_id;
        ELSE
            UPDATE platform_workspace_metadata
            SET simplefin_configured = TRUE,
                simplefin_last_synced_at = NEW.last_synced_at,
                simplefin_last_sync_status = NEW.last_sync_status,
                simplefin_last_sync_error = NEW.last_sync_error
            WHERE workspace_id = NEW.workspace_id;
        END IF;
        PERFORM set_config('app.platform_admin_mode', COALESCE(prev_mode, ''), true);
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('app.platform_admin_mode', COALESCE(prev_mode, ''), true);
        RAISE;
    END;
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

DROP TRIGGER IF EXISTS trg_sync_platform_workspace_simplefin ON simplefin_connections;
CREATE TRIGGER trg_sync_platform_workspace_simplefin
AFTER INSERT OR UPDATE OR DELETE ON simplefin_connections
FOR EACH ROW EXECUTE FUNCTION sync_platform_workspace_simplefin();

-- Recreate admin_workspace_overview over metadata-only tables (no FORCE RLS dependency on workspaces)
DROP VIEW IF EXISTS admin_workspace_overview;
CREATE VIEW admin_workspace_overview AS
SELECT
    pwm.workspace_id,
    pwm.workspace_name,
    pwm.created_at,
    COUNT(wm.user_id)::INT AS member_count,
    COALESCE(
        MAX(CASE WHEN wm.role = 'owner' THEN pu.email END),
        (SELECT pu2.email FROM platform_users pu2 WHERE pu2.user_id = pwm.owner_user_id)
    ) AS owner_email,
    pwm.simplefin_last_synced_at,
    CASE
        WHEN pwm.simplefin_configured THEN pwm.simplefin_last_sync_status
        ELSE 'NOT_CONFIGURED'
    END AS simplefin_status,
    pwm.simplefin_last_sync_error AS simplefin_error
FROM platform_workspace_metadata pwm
LEFT JOIN platform_workspace_members wm ON wm.workspace_id = pwm.workspace_id
LEFT JOIN platform_users pu ON pu.user_id = wm.user_id
WHERE is_platform_admin_mode()
  AND pwm.is_demo = FALSE
GROUP BY
    pwm.workspace_id,
    pwm.workspace_name,
    pwm.owner_user_id,
    pwm.created_at,
    pwm.simplefin_configured,
    pwm.simplefin_last_synced_at,
    pwm.simplefin_last_sync_status,
    pwm.simplefin_last_sync_error;

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
        COALESCE(COUNT(DISTINCT CASE WHEN pwm.workspace_id IS NOT NULL AND wm.role = 'owner' THEN pwm.workspace_id END), 0)::INT AS owned_workspace_count,
        COALESCE(COUNT(DISTINCT CASE WHEN pwm.workspace_id IS NOT NULL AND wm.role <> 'owner' THEN pwm.workspace_id END), 0)::INT AS member_workspace_count
    FROM platform_users pu
    LEFT JOIN platform_workspace_members wm ON wm.user_id = pu.user_id
    LEFT JOIN platform_workspace_metadata pwm ON pwm.workspace_id = wm.workspace_id AND pwm.is_demo = FALSE
    GROUP BY pu.user_id, pu.email, pu.display_name, pu.is_active, pu.last_login_at, pu.created_at
    ORDER BY pu.created_at DESC;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

CREATE OR REPLACE FUNCTION admin_delete_user(target_user_id UUID) RETURNS BOOLEAN AS $$
DECLARE
    deleted_count INT;
    prev_user_id TEXT;
    prev_ws_id TEXT;
BEGIN
    IF NOT is_platform_admin_mode() THEN
        RAISE EXCEPTION 'platform_admin_mode_required';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM platform_users WHERE user_id = target_user_id) THEN
        RETURN FALSE;
    END IF;

    prev_user_id := current_setting('app.current_user_id', true);
    prev_ws_id := current_setting('app.current_workspace_id', true);

    BEGIN
        PERFORM set_config('app.current_user_id', target_user_id::text, true);
        PERFORM set_config('app.current_workspace_id', '', true);

        -- 1. Delete non-demo workspaces owned by target_user_id (ON DELETE CASCADE removes ledger rows)
        INSERT INTO user_preferences (user_id, demo_mode_enabled)
        VALUES (target_user_id, FALSE)
        ON CONFLICT (user_id) DO UPDATE SET demo_mode_enabled = FALSE;

        DELETE FROM workspaces w
        WHERE w.owner_user_id = target_user_id
           OR EXISTS (
               SELECT 1
               FROM workspace_members wm
               WHERE wm.workspace_id = w.id
                 AND wm.user_id = target_user_id
                 AND wm.role = 'owner'
           );

        -- 2. Delete demo sandboxes owned by target_user_id
        UPDATE user_preferences SET demo_mode_enabled = TRUE WHERE user_id = target_user_id;

        DELETE FROM workspaces w
        WHERE w.owner_user_id = target_user_id
           OR EXISTS (
               SELECT 1
               FROM workspace_members wm
               WHERE wm.workspace_id = w.id
                 AND wm.user_id = target_user_id
                 AND wm.role = 'owner'
           );

        -- 3. Remove target_user_id from remaining shared non-demo workspaces (roster BEFORE members)
        UPDATE user_preferences SET demo_mode_enabled = FALSE WHERE user_id = target_user_id;
        DELETE FROM workspace_roster WHERE user_id = target_user_id;
        DELETE FROM workspace_members WHERE user_id = target_user_id;
        DELETE FROM push_subscriptions WHERE user_id = target_user_id;
        DELETE FROM user_preferences WHERE user_id = target_user_id;

        PERFORM set_config('app.current_user_id', COALESCE(prev_user_id, ''), true);
        PERFORM set_config('app.current_workspace_id', COALESCE(prev_ws_id, ''), true);
    EXCEPTION WHEN OTHERS THEN
        PERFORM set_config('app.current_user_id', COALESCE(prev_user_id, ''), true);
        PERFORM set_config('app.current_workspace_id', COALESCE(prev_ws_id, ''), true);
        RAISE;
    END;

    DELETE FROM platform_users WHERE user_id = target_user_id;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;

    RETURN deleted_count > 0;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'budget_app_user') THEN
        GRANT ALL PRIVILEGES ON platform_workspace_metadata, platform_workspace_members, admin_workspace_overview TO budget_app_user;
        GRANT EXECUTE ON FUNCTION
            sync_platform_workspace_metadata_from_workspace(),
            sync_platform_workspace_members(),
            sync_platform_workspace_simplefin(),
            admin_list_users(),
            admin_delete_user(UUID)
        TO budget_app_user;
    END IF;
END
$$;
