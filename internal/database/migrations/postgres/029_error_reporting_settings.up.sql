-- Optional Sentry error reporting for the GoForge control plane (Settings →
-- Profile → Error reporting, t1770u). One row at most (id = 1). The DSN is
-- sealed with ENCRYPTION_KEY (AES-GCM, base64) like notify_settings.url_sealed:
-- it carries the project's public key, and anyone holding it can send events
-- and burn quota. '' means no DSN, which turns error reporting off.
-- environment is the Sentry environment tag; '' means automatic
-- (development in dev mode, otherwise production).
CREATE TABLE IF NOT EXISTS error_reporting_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    dsn_sealed TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
