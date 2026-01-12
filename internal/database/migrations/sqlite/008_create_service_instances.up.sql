-- Service Instances table (needed before containers due to FK)
CREATE TABLE service_instances (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    template_name TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT,
    config TEXT DEFAULT '{}',
    domain TEXT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'starting', 'running', 'stopped', 'failed')),
    connection_info TEXT DEFAULT '{}',
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_service_instances_user_id ON service_instances(user_id);
CREATE INDEX idx_service_instances_template_name ON service_instances(template_name);
