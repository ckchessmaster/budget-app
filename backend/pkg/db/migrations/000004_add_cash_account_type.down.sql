-- PostgreSQL cannot drop a single value from an enum without recreating the type.
-- Keep rollback as a safe no-op to avoid breaking existing 'cash' rows.
SELECT 1;
