-- Allows backups on a service page. The nightly run and `goforge backup run`
-- include a postgres, gitea, or vaultwarden instance only when this is true
-- and its container is running. Other templates are still skipped. Existing
-- instances of those three templates stay in the run.
ALTER TABLE service_instances
    ADD COLUMN IF NOT EXISTS backup_enabled BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE service_instances
SET backup_enabled = TRUE
WHERE template_name IN ('postgres', 'gitea', 'vaultwarden');
