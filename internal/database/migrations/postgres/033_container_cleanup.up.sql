-- Records host cleanup before Docker removal so a transient container-row
-- failure remains visible to deletion retries and orphan reconciliation.
CREATE TABLE IF NOT EXISTS container_cleanup (
    docker_id TEXT PRIMARY KEY,
    project_id UUID,
    environment_id UUID,
    reason TEXT NOT NULL CHECK (reason in ('delete', 'orphan', 'create_failure')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_container_cleanup_project ON container_cleanup(project_id);
CREATE INDEX IF NOT EXISTS idx_container_cleanup_environment ON container_cleanup(environment_id);
