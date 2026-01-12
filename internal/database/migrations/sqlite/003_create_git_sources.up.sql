-- Git Sources table
CREATE TABLE git_sources (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('github', 'gitlab', 'gitea', 'ssh')),
    api_url TEXT,
    access_token TEXT,
    ssh_private_key TEXT,
    is_default INTEGER DEFAULT 0,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_git_sources_user_id ON git_sources(user_id);
