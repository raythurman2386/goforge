-- Deployment Queue table for persistent deployment job processing
CREATE TABLE deployment_queue (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    deployment_id UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT
);

CREATE INDEX idx_deployment_queue_status ON deployment_queue(status);
CREATE INDEX idx_deployment_queue_created_at ON deployment_queue(created_at);
CREATE INDEX idx_deployment_queue_deployment_id ON deployment_queue(deployment_id);
