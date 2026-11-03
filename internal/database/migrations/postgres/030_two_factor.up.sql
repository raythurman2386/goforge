-- Opt-in TOTP two-factor authentication for local (password) accounts
-- (v0.5.0 S4, t1873u). GitHub OAuth sign-in does not use these tables: it
-- relies on GitHub's own 2FA.
--
-- user_totp holds one row per account that started enrollment. The secret
-- is sealed with ENCRYPTION_KEY (AES-GCM, base64) like the other stored
-- secrets. enabled_at is NULL while enrollment waits for the first code and
-- is set once a code is confirmed. last_used_step is the highest TOTP time
-- step accepted, so a code cannot be replayed.
CREATE TABLE IF NOT EXISTS user_totp (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_sealed TEXT NOT NULL,
    enabled_at TIMESTAMPTZ,
    last_used_step BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One-time recovery codes, stored only as SHA-256 hashes of the normalized
-- code. used_at is set when a code signs in or turns 2FA off.
CREATE TABLE IF NOT EXISTS user_recovery_codes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash VARCHAR(64) NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, code_hash)
);

-- A password sign-in for an account with 2FA on creates a short-lived
-- challenge instead of a session. The browser holds the random token in the
-- goforge_2fa cookie; only its SHA-256 hash is stored. attempts counts wrong
-- codes against this challenge.
CREATE TABLE IF NOT EXISTS login_challenges (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    token_hash VARCHAR(64) UNIQUE NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    remember_me BOOLEAN NOT NULL DEFAULT FALSE,
    attempts INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_login_challenges_user_id ON login_challenges(user_id);
CREATE INDEX IF NOT EXISTS idx_login_challenges_expires_at ON login_challenges(expires_at);
