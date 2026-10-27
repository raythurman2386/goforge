-- Email identity is case-insensitive. Store a lowercase address and reject
-- a second account that differs only by case. Existing mixed-case rows are
-- folded first. Two accounts that already collide ignoring case must be
-- merged before this migration can finish.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users
        GROUP BY lower(trim(email))
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'case-insensitive duplicate emails exist; merge them before migrating';
    END IF;
END $$;

UPDATE users
SET email = lower(trim(email))
WHERE email IS DISTINCT FROM lower(trim(email));

CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_key ON users (lower(email));
