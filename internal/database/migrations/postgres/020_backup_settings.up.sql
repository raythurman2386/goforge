-- Instance-wide backup destination and policy, edited by an admin in
-- Settings → Profile. One row at most (id = 1). Once the row exists it wins
-- over the BACKUP_* environment; with no row GoForge uses the environment.
-- The access key ID and secret are sealed with ENCRYPTION_KEY (AES-GCM,
-- base64), like git source tokens; '' means not set.
CREATE TABLE backup_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    endpoint TEXT NOT NULL DEFAULT '',
    region TEXT NOT NULL DEFAULT 'auto',
    bucket TEXT NOT NULL DEFAULT '',
    prefix TEXT NOT NULL DEFAULT 'goforge/',
    access_key_id_sealed TEXT NOT NULL DEFAULT '',
    secret_access_key_sealed TEXT NOT NULL DEFAULT '',
    force_path_style BOOLEAN NOT NULL DEFAULT FALSE,
    schedule TEXT NOT NULL DEFAULT '02:00 America/Chicago',
    retention_daily INTEGER NOT NULL DEFAULT 7 CHECK (retention_daily BETWEEN 1 AND 366),
    timeout_seconds BIGINT NOT NULL DEFAULT 7200 CHECK (timeout_seconds > 0),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
