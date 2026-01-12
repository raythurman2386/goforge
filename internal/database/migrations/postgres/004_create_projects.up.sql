-- Projects table
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    git_source_id UUID REFERENCES git_sources(id) ON DELETE SET NULL,
    repository_url TEXT NOT NULL,
    repository_name VARCHAR(255),
    default_branch VARCHAR(255) DEFAULT 'main',
    dockerfile_path VARCHAR(255) DEFAULT 'Dockerfile',
    docker_compose_path VARCHAR(255),
    build_context VARCHAR(255) DEFAULT '.',
    build_args JSONB DEFAULT '{}',
    health_check_path VARCHAR(255) DEFAULT '/',
    health_check_interval INTEGER DEFAULT 30,
    auto_deploy BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_projects_user_id ON projects(user_id);
CREATE INDEX idx_projects_git_source_id ON projects(git_source_id);
