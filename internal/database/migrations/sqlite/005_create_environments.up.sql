-- Environments table
CREATE TABLE environments (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    domain TEXT,
    port INTEGER,
    branch TEXT,
    replicas INTEGER DEFAULT 1,
    cpu_limit TEXT,
    memory_limit TEXT,
    auto_deploy INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    UNIQUE(project_id, name)
);

CREATE INDEX idx_environments_project_id ON environments(project_id);
CREATE INDEX idx_environments_domain ON environments(domain);
