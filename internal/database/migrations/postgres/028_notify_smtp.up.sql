-- Email (SMTP) notifier (t1676u). kind gains 'email'. The SMTP host,
-- username, password, From, and To list are sealed with ENCRYPTION_KEY like
-- url_sealed and token_sealed; '' means not set. Port and TLS mode are not
-- secret. smtp_tls is 'starttls' (upgrade a plain connection, usually port
-- 587), 'implicit' (implicit TLS / SMTPS, usually 465), or 'none' (plain; only
-- allowed with a username on a loopback host).
ALTER TABLE notify_settings DROP CONSTRAINT IF EXISTS notify_settings_kind_check;
ALTER TABLE notify_settings ADD CONSTRAINT notify_settings_kind_check
    CHECK (kind IN ('ntfy', 'webhook', 'email'));
ALTER TABLE notify_settings
    ADD COLUMN IF NOT EXISTS smtp_host_sealed TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS smtp_port INTEGER NOT NULL DEFAULT 0 CHECK (smtp_port >= 0 AND smtp_port <= 65535),
    ADD COLUMN IF NOT EXISTS smtp_tls TEXT NOT NULL DEFAULT 'starttls' CHECK (smtp_tls IN ('starttls', 'implicit', 'none')),
    ADD COLUMN IF NOT EXISTS smtp_username_sealed TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS smtp_password_sealed TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS smtp_from_sealed TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS smtp_to_sealed TEXT NOT NULL DEFAULT '';
