-- Instance-wide outbound notifier (Settings → Profile → Notifications). One
-- row at most (id = 1). kind is 'ntfy' (POST to a topic URL) or 'webhook'
-- (POST a JSON body). The URL and the optional auth token are sealed with
-- ENCRYPTION_KEY (AES-GCM, base64) because an ntfy topic or a webhook path
-- is itself a secret; '' means not set.
CREATE TABLE IF NOT EXISTS notify_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    kind TEXT NOT NULL DEFAULT 'ntfy' CHECK (kind IN ('ntfy', 'webhook')),
    url_sealed TEXT NOT NULL DEFAULT '',
    token_sealed TEXT NOT NULL DEFAULT '',
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
