-- Reverse 031. Triggers go first so the orphan-user UPDATE below is allowed.
-- user_id values whose user is already gone are set NULL, which is what
-- ON DELETE SET NULL used to do, so the foreign key can be restored.

DROP TRIGGER IF EXISTS audit_logs_no_mutate ON audit_logs;
DROP TRIGGER IF EXISTS audit_logs_no_truncate ON audit_logs;
DROP TRIGGER IF EXISTS audit_log_seals_no_mutate ON audit_log_seals;
DROP TRIGGER IF EXISTS audit_log_seals_no_truncate ON audit_log_seals;
DROP FUNCTION IF EXISTS goforge_audit_forbid_mutation();

DROP TABLE IF EXISTS audit_log_seals;
DROP TABLE IF EXISTS audit_settings;

UPDATE audit_logs
SET user_id = NULL
WHERE user_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = audit_logs.user_id);

ALTER TABLE audit_logs
    ADD CONSTRAINT audit_logs_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;
