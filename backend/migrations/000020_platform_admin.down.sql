DROP FUNCTION IF EXISTS admin_delete_user(UUID);
DROP FUNCTION IF EXISTS admin_list_users();
DROP VIEW IF EXISTS admin_workspace_overview;

DROP POLICY IF EXISTS platform_invitations_isolation ON platform_invitations;
DROP TABLE IF EXISTS platform_invitations;

DROP FUNCTION IF EXISTS current_app_platform_invite_token_hash();
DROP FUNCTION IF EXISTS is_platform_admin_mode();

DROP TABLE IF EXISTS outbound_email_logs;

ALTER TABLE simplefin_connections
    DROP COLUMN IF EXISTS last_sync_error,
    DROP COLUMN IF EXISTS last_sync_status;

DROP TABLE IF EXISTS platform_users;
DROP TYPE IF EXISTS platform_invite_status;
