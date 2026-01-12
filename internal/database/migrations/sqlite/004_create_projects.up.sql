-- Projects table
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    git_source_id TEXT REFERENCES git_sources(id) ON DELETE SET NULL,
    repository_url TEXT NOT NULL,
    repository_name TEXT,
    default_branch TEXT DEFAULT 'main',
    dockerfile_path TEXT DEFAULT 'Dockerfile',
    docker_compose_path TEXT,
    build_context TEXT DEFAULT '.',
    build_args TEXT DEFAULT '{}',
    health_check_path TEXT DEFAULT '/',
    health_check_interval INTEGER DEFAULT 30,
    auto_deploy INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_projects_user_id ON projects(user_id);
CREATE INDEX idx_projects_git_source_id ON projects(git_source_id);
