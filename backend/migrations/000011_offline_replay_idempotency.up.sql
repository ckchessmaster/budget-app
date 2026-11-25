-- Idempotency key for the durable offline replay [TASK-5.2.4].
--
-- POST /api/v1/transactions/sync used to create every queued item inside the request, so a
-- batch that died half way through left the browser to resend the whole thing -- and an item
-- that had already landed would be created a second time. The replay now runs as a Temporal
-- workflow, where activities are at-least-once by definition. The only thing that makes a
-- retry safe is a key the client generated before the first attempt.
--
-- VARCHAR rather than UUID: the ids already queued in users' IndexedDB are prefixed
-- ("offline-<uuid>"), and a key an existing client cannot send is a key that duplicates.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS client_id VARCHAR(128);

-- Unique per workspace, and only over the rows that carry a key. Bank-feed rows and split
-- children have none, and those NULLs must not collide with each other.
CREATE UNIQUE INDEX IF NOT EXISTS uq_transactions_workspace_client_id
    ON transactions (workspace_id, client_id)
    WHERE client_id IS NOT NULL;
