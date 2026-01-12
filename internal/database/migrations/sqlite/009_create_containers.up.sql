-- Containers table
CREATE TABLE containers (
    id TEXT PRIMARY KEY,
    deployment_id TEXT REFERENCES deployments(id) ON DELETE SET NULL,
    service_instance_id TEXT REFERENCES service_instances(id) ON DELETE SET NULL,
    docker_id TEXT NOT NULL,
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    status TEXT NOT NULL,
    ports TEXT DEFAULT '[]',
    labels TEXT DEFAULT '{}',
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_containers_deployment_id ON containers(deployment_id);
CREATE INDEX idx_containers_service_instance_id ON containers(service_instance_id);
CREATE INDEX idx_containers_docker_id ON containers(docker_id);

-- Container Metrics table (time-series)
CREATE TABLE container_metrics (
    id TEXT PRIMARY KEY,
    container_id TEXT NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    timestamp TEXT DEFAULT (datetime('now')),
    cpu_percent REAL,
    memory_usage INTEGER,
    memory_limit INTEGER,
    memory_percent REAL,
    network_rx INTEGER,
    network_tx INTEGER,
    block_read INTEGER,
    block_write INTEGER
);

CREATE INDEX idx_container_metrics_container_timestamp ON container_metrics(container_id, timestamp DESC);
