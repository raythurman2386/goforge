-- An email notifier cannot survive the old kind check: turn it off and
-- fall back to ntfy with whatever URL was stored (usually none).
UPDATE notify_settings SET kind = 'ntfy', enabled = FALSE WHERE kind = 'email';
ALTER TABLE notify_settings
    DROP COLUMN IF EXISTS smtp_host_sealed,
    DROP COLUMN IF EXISTS smtp_port,
    DROP COLUMN IF EXISTS smtp_tls,
    DROP COLUMN IF EXISTS smtp_username_sealed,
    DROP COLUMN IF EXISTS smtp_password_sealed,
    DROP COLUMN IF EXISTS smtp_from_sealed,
    DROP COLUMN IF EXISTS smtp_to_sealed;
ALTER TABLE notify_settings DROP CONSTRAINT IF EXISTS notify_settings_kind_check;
ALTER TABLE notify_settings ADD CONSTRAINT notify_settings_kind_check
    CHECK (kind IN ('ntfy', 'webhook'));
