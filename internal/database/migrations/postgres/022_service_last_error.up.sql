-- Why a service instance last failed (a failed image pull, a container that
-- did not start, a post_start hook that exited non-zero), so the API, MCP,
-- and the service page can say more than "failed". Secret config values are
-- redacted before the text is stored. NULL when there is nothing to report.
ALTER TABLE service_instances
    ADD COLUMN IF NOT EXISTS last_error TEXT;
