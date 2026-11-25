DROP INDEX IF EXISTS idx_simplefin_connections_pending_claim;
ALTER TABLE simplefin_connections
    DROP COLUMN IF EXISTS pending_claim_token;
