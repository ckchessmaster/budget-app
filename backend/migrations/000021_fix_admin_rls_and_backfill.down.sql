DROP TRIGGER IF EXISTS trg_sync_platform_workspace_simplefin ON simplefin_connections;
DROP TRIGGER IF EXISTS trg_sync_platform_workspace_members ON workspace_members;
DROP TRIGGER IF EXISTS trg_sync_platform_workspace_metadata ON workspaces;

DROP FUNCTION IF EXISTS sync_platform_workspace_simplefin();
DROP FUNCTION IF EXISTS sync_platform_workspace_members();
DROP FUNCTION IF EXISTS sync_platform_workspace_metadata_from_workspace();

DROP VIEW IF EXISTS admin_workspace_overview;
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

DROP POLICY IF EXISTS platform_workspace_members_isolation ON platform_workspace_members;
DROP TABLE IF EXISTS platform_workspace_members;

DROP POLICY IF EXISTS platform_workspace_metadata_isolation ON platform_workspace_metadata;
DROP TABLE IF EXISTS platform_workspace_metadata;
