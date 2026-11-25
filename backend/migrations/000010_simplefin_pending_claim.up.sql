-- Staging column for an asynchronous SimpleFIN claim.
--
-- The claim used to happen inside POST /api/v1/bank/simplefin/claim, which meant a slow
-- or rate-limited bank held the browser's request open and a failure after the claim left
-- a spent token with nothing retrying it. Moving the claim into a Temporal workflow runs
-- into another constraint: Temporal records workflow inputs in history, which operators
-- can read and which is retained for the whole retention window, so a setup token cannot
-- travel as a workflow argument.
--
-- The API therefore seals the token here and passes only the workspace scope. The column
-- is cleared once the worker has exchanged it for an access URL, which also makes the
-- claim idempotent: a late retry finds nothing pending instead of spending a token twice.
-- A failed claim leaves the previously working encrypted_access_url untouched.
ALTER TABLE simplefin_connections
    ADD COLUMN IF NOT EXISTS pending_claim_token TEXT;

CREATE INDEX IF NOT EXISTS idx_simplefin_connections_pending_claim
    ON simplefin_connections (workspace_id)
    WHERE pending_claim_token IS NOT NULL;
