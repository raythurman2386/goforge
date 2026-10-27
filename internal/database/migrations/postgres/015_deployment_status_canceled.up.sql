-- Align the deployments status check with the application value "canceled".
-- Existing databases created from 007 used the British spelling "cancelled".

UPDATE deployments SET status = 'canceled' WHERE status = 'cancelled';

DO $$
DECLARE
    constraint_name text;
BEGIN
    SELECT con.conname INTO constraint_name
    FROM pg_constraint con
    JOIN pg_class rel ON rel.oid = con.conrelid
    WHERE rel.relname = 'deployments'
      AND con.contype = 'c'
      AND pg_get_constraintdef(con.oid) ILIKE '%status%'
      AND pg_get_constraintdef(con.oid) ILIKE '%pending%'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE deployments DROP CONSTRAINT %I', constraint_name);
    END IF;
END $$;

ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('pending', 'cloning', 'building', 'deploying', 'running', 'failed', 'rolled_back', 'canceled'));
