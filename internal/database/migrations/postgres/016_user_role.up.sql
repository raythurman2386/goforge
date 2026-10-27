-- Account role. The earliest account becomes the operator so an
-- existing install keeps host actions. Later rows stay users.
-- New databases assign the role in the insert transaction.

ALTER TABLE users
    ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'user'
    CHECK (role IN ('admin', 'user'));

UPDATE users
SET role = 'admin'
WHERE id = (
    SELECT id FROM users
    ORDER BY created_at ASC, id ASC
    LIMIT 1
);
