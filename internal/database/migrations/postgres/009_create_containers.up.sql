-- Containers table
CREATE TABLE containers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    deployment_id UUID REFERENCES deployments(id) ON DELETE SET NULL,
    service_instance_id UUID REFERENCES service_instances(id) ON DELETE SET NULL,
    docker_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    image VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    ports JSONB DEFAULT '[]',
    labels JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_containers_deployment_id ON containers(deployment_id);
CREATE INDEX idx_containers_service_instance_id ON containers(service_instance_id);
CREATE INDEX idx_containers_docker_id ON containers(docker_id);

-- Container Metrics table (time-series)
CREATE TABLE container_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ DEFAULT NOW(),
    cpu_percent DECIMAL(5,2),
    memory_usage BIGINT,
    memory_limit BIGINT,
    memory_percent DECIMAL(5,2),
    network_rx BIGINT,
    network_tx BIGINT,
    block_read BIGINT,
    block_write BIGINT
);

CREATE INDEX idx_container_metrics_container_timestamp ON container_metrics(container_id, timestamp DESC);
