-- Allows backups on a service page. The nightly run and `goforge backup run`
-- include an instance whose template declares a usable backup.strategy
-- only when this is true and its container is running. Other templates are
-- still skipped. Existing postgres, gitea, and vaultwarden instances start
-- on; existing minio and every other template start off. A down then up
-- resets any opt-outs.
ALTER TABLE service_instances
    ADD COLUMN IF NOT EXISTS backup_enabled BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE service_instances
SET backup_enabled = TRUE
WHERE template_name IN ('postgres', 'gitea', 'vaultwarden');
