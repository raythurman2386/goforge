-- Environments table
CREATE TABLE environments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(50) NOT NULL,
    domain VARCHAR(255),
    port INTEGER,
    branch VARCHAR(255),
    replicas INTEGER DEFAULT 1,
    cpu_limit VARCHAR(20),
    memory_limit VARCHAR(20),
    auto_deploy BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(project_id, name)
);

CREATE INDEX idx_environments_project_id ON environments(project_id);
CREATE INDEX idx_environments_domain ON environments(domain);
