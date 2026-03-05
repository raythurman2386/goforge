-- Join table linking projects to service instances (many-to-many)
CREATE TABLE project_service_instances (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    service_instance_id UUID NOT NULL REFERENCES service_instances(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (project_id, service_instance_id)
);

CREATE INDEX idx_project_service_instances_project_id ON project_service_instances(project_id);
CREATE INDEX idx_project_service_instances_service_instance_id ON project_service_instances(service_instance_id);
