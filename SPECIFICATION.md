# GoForge — Technical Design Specification

## 1. Project Overview

**Summary:** GoForge is a lightweight, self-hosted PaaS built entirely in Go with HTMX + templ for the frontend. It reimplements the core Coolify experience (git-to-deploy, one-click services, databases, SSL, monitoring) with a focus on minimal resource usage, simplicity, and a hypermedia-driven architecture.

**Core Objectives:**
- Provide git-based deployment workflows for Docker/Compose applications
- Enable one-click deployment of common services (databases, analytics, etc.)
- Automatic reverse-proxy configuration with Let's Encrypt SSL via Traefik
- Real-time logs and resource monitoring via SSE
- Zero client-side JavaScript beyond HTMX

**Target Users:** Developers and small teams who want self-hosted deployment infrastructure without the complexity of Kubernetes or the resource overhead of heavier PaaS solutions.

**Key Decisions (v1):**
- Reverse Proxy: Traefik (Docker-native integration)
- Deployment Target: Single server only
- Authentication: GitHub OAuth (primary), local accounts (fallback)
- Database: PostgreSQL from day one
- Service Templates: Loaded from configurable directory for user customization

---

## 2. Requirements

### Functional Requirements

| ID | Requirement |
|----|-------------|
| F1 | User authentication via GitHub OAuth (primary) and local accounts |
| F2 | Project/environment CRUD (dev, staging, prod) |
| F3 | Git source configuration (GitHub, GitLab, Gitea, SSH) |
| F4 | Docker & Docker Compose build/deploy pipeline |
| F5 | One-click service catalog (PostgreSQL, Redis, etc.) from configurable directory |
| F6 | Automatic Traefik reverse-proxy + Let's Encrypt SSL |
| F7 | Real-time deployment logs via SSE |
| F8 | Resource monitoring (CPU, memory, disk per container) |
| F9 | Environment variable management with secrets |
| F10 | Deployment rollback capability |

### Non-Functional Requirements

| ID | Requirement |
|----|-------------|
| NF1 | Single binary deployment (Go) |
| NF2 | < 100MB RAM idle footprint |
| NF3 | Sub-200ms page loads (server-rendered) |
| NF4 | PostgreSQL for data persistence |
| NF5 | No client-side build toolchain required at runtime |
| NF6 | Graceful degradation without JavaScript |

### Success Criteria

- Deploy a GitHub repo to production with SSL in < 5 minutes
- Dashboard fully functional with JavaScript disabled (except real-time features)
- Memory usage < 50% of equivalent Coolify installation
- 90%+ test coverage on core deployment logic

---

## 3. Cursor Task List

This checklist is ordered for incremental, testable implementation. Each task is independently executable.

### Phase 1: Project Foundation

- [x] **1.1** Initialize Go module: `go mod init github.com/raythurman2386/goforge`
- [x] **1.2** Create directory structure as specified in Section 4
- [x] **1.3** Add core dependencies to go.mod:
  - `github.com/go-chi/chi/v5` (router)
  - `github.com/a-h/templ` (templating)
  - `github.com/lib/pq` (PostgreSQL)
  - `github.com/golang-migrate/migrate/v4` (migrations)
  - `golang.org/x/crypto` (security)
- [x] **1.4** Create `internal/config/config.go` with struct for all configuration options
- [x] **1.5** Implement config loading from environment variables with sensible defaults
- [x] **1.6** Create `cmd/goforge/main.go` entry point with config loading and graceful shutdown
- [x] **1.7** Set up structured logging with `log/slog` in `internal/logger/logger.go`
- [x] **1.8** Download TailwindCSS standalone CLI, create build script `scripts/tailwind.sh`
- [x] **1.9** Create `tailwind.config.js` with content paths for templ files
- [x] **1.10** Create base layout template `internal/web/templates/layouts/base.templ`
- [x] **1.11** Add HTMX 2.x to `static/js/htmx.min.js`
- [x] **1.12** Create HTTP router in `internal/web/router.go` using chi
- [x] **1.13** Add common middleware stack (logging, recovery, request ID)
- [x] **1.14** Create health check endpoint `GET /health`
- [x] **1.15** Create Makefile with targets: `build`, `dev`, `css`, `templ`, `migrate`

### Phase 2: Database & Models

- [x] **2.1** Create `internal/database/database.go` with PostgreSQL connection pool
- [x] **2.2** Implement connection retry logic with exponential backoff
- [x] **2.3** Set up golang-migrate in `internal/database/migrations.go`
- [x] **2.4** Create migration `001_create_users.up.sql` and `.down.sql`
- [x] **2.5** Create migration `002_create_sessions.up.sql` and `.down.sql`
- [x] **2.6** Create migration `003_create_git_sources.up.sql` and `.down.sql`
- [x] **2.7** Create migration `004_create_projects.up.sql` and `.down.sql`
- [x] **2.8** Create migration `005_create_environments.up.sql` and `.down.sql`
- [x] **2.9** Create migration `006_create_env_variables.up.sql` and `.down.sql`
- [x] **2.10** Create migration `007_create_deployments.up.sql` and `.down.sql`
- [x] **2.11** Create migration `008_create_containers.up.sql` and `.down.sql`
- [x] **2.12** Create migration `009_create_service_instances.up.sql` and `.down.sql`
- [x] **2.13** Create migration `010_create_audit_logs.up.sql` and `.down.sql`
- [x] **2.14** Create `internal/models/` package with Go structs matching schema
- [x] **2.15** Implement repository pattern in `internal/database/repositories/`

### Phase 3: Authentication

- [x] **3.1** Create `internal/auth/auth.go` with core auth interfaces
- [x] **3.2** Implement password hashing with Argon2id in `internal/auth/password.go`
- [x] **3.3** Create session management in `internal/auth/session.go`
- [x] **3.4** Implement secure cookie handling (HTTP-only, SameSite, Secure)
- [x] **3.5** Create CSRF middleware in `internal/auth/csrf.go`
- [x] **3.6** Implement GitHub OAuth flow in `internal/auth/github.go`:
  - Authorization URL generation
  - Token exchange
  - User info fetching
  - Account linking/creation
- [x] **3.7** Create auth middleware in `internal/auth/middleware.go`
- [x] **3.8** Build login page template `internal/web/templates/pages/login.templ`
- [x] **3.9** Create login handler with GitHub OAuth button
- [x] **3.10** Implement OAuth callback handler
- [x] **3.11** Add local account registration (fallback)
- [x] **3.12** Create logout handler with session cleanup
- [x] **3.13** Add "remember me" functionality with extended session TTL

### Phase 4: Core UI Components

- [x] **4.1** Create navigation component `internal/web/templates/components/nav.templ`
- [x] **4.2** Create sidebar component with project list
- [x] **4.3** Build notification/toast component for HTMX responses
- [x] **4.4** Create modal component (HTMX-driven)
- [x] **4.5** Build form components (input, select, checkbox, button)
- [x] **4.6** Create table component with sorting support
- [ ] **4.7** Build pagination component
- [x] **4.8** Create loading spinner component
- [x] **4.9** Build empty state component
- [x] **4.10** Create error page templates (404, 500, 403)

### Phase 5: Project Management

- [x] **5.1** Create `internal/project/service.go` with project CRUD operations
- [x] **5.2** Implement project list handler `GET /projects`
- [x] **5.3** Create project list page `internal/web/templates/pages/projects/list.templ`
- [x] **5.4** Implement project creation form and handler `POST /projects`
- [x] **5.5** Create project detail page with tabbed interface
- [x] **5.6** Implement environment service in `internal/project/environment.go`
- [x] **5.7** Build environment switcher component
- [x] **5.8** Create environment creation modal
- [x] **5.9** Implement env variable editor with add/edit/delete
- [x] **5.10** Add secret masking toggle for sensitive variables
- [x] **5.11** Create project settings page
- [x] **5.12** Implement project deletion with confirmation modal
- [ ] **5.13** Add project search/filter functionality

### Phase 6: Git Integration

- [x] **6.1** Create `internal/git/service.go` with git source interface
- [x] **6.2** Implement GitHub API client in `internal/git/github.go`:
  - Repository listing
  - Branch listing
  - Commit fetching
  - Webhook management
- [ ] **6.3** Create GitLab API client in `internal/git/gitlab.go`
- [ ] **6.4** Create Gitea API client in `internal/git/gitea.go`
- [x] **6.5** Implement SSH key management in `internal/git/ssh.go`
- [x] **6.6** Create git source configuration page
- [ ] **6.7** Build repository browser component (HTMX-powered)
- [ ] **6.8** Create branch/tag selector dropdown
- [ ] **6.9** Implement webhook receiver in `internal/web/handlers/webhooks.go`
- [ ] **6.10** Add webhook signature verification (GitHub, GitLab, Gitea)
- [x] **6.11** Create git clone/pull operations in `internal/git/operations.go`
- [ ] **6.12** Implement shallow clone for faster deployments

### Phase 7: Docker Integration

- [x] **7.1** Create `internal/docker/client.go` wrapping Docker SDK
- [ ] **7.2** Implement connection health check and reconnection
- [x] **7.3** Create image builder in `internal/docker/build.go`:
  - Dockerfile detection
  - Build context preparation
  - Layer caching configuration
  - Progress streaming
- [ ] **7.4** Implement docker-compose parser in `internal/docker/compose.go`
- [x] **7.5** Create container manager in `internal/docker/container.go`:
  - Create, start, stop, restart, remove
  - Environment variable injection
  - Port mapping
  - Volume mounting
- [x] **7.6** Implement log streaming in `internal/docker/logs.go`
- [ ] **7.7** Create network manager in `internal/docker/network.go`
- [ ] **7.8** Implement volume manager in `internal/docker/volume.go`
- [ ] **7.9** Add image cleanup/pruning functionality
- [ ] **7.10** Create Docker event listener for container state changes

### Phase 8: Deployment Pipeline

- [x] **8.1** Create `internal/deploy/service.go` with deployment orchestration
- [x] **8.2** Implement deployment state machine:
  - pending → cloning → building → deploying → running
  - Any state → failed
  - running → rolled_back
- [x] **8.3** Create deployment queue in `internal/deploy/queue.go`
- [x] **8.4** Implement concurrent deployment limiting (configurable)
- [x] **8.5** Create build pipeline in `internal/deploy/pipeline.go`:
  - Clone repository
  - Prepare build context
  - Build image
  - Create container
  - Health check
  - Switch traffic
- [x] **8.6** Implement blue-green deployment strategy
- [x] **8.7** Add health check verification with configurable endpoint
- [x] **8.8** Create rollback mechanism in `internal/deploy/rollback.go`
- [ ] **8.9** Implement deployment cleanup (old images, containers)
- [x] **8.10** Create deployment history page
- [x] **8.11** Build deployment detail view with log viewer
- [x] **8.12** Add manual deployment trigger button

### Phase 9: Traefik Integration

- [x] **9.1** Create `internal/proxy/traefik.go` for Traefik management
- [ ] **9.2** Implement Docker labels generation for Traefik routing
- [ ] **9.3** Create Let's Encrypt certificate resolver configuration
- [ ] **9.4** Implement domain validation
- [ ] **9.5** Add custom domain management UI
- [ ] **9.6** Create SSL certificate status checking
- [ ] **9.7** Implement Traefik dashboard proxy (optional, secured)
- [ ] **9.8** Add HTTP to HTTPS redirect configuration
- [ ] **9.9** Create middleware configuration (rate limiting, headers)
- [ ] **9.10** Implement wildcard subdomain support

### Phase 10: Real-Time Features (SSE)

- [x] **10.1** Create SSE hub in `internal/sse/hub.go`:
  - Client connection management
  - Topic-based subscriptions
  - Broadcast functionality
- [x] **10.2** Implement SSE client handler in `internal/sse/client.go`
- [ ] **10.3** Create heartbeat mechanism for connection keep-alive
- [x] **10.4** Implement deployment log streaming endpoint
- [x] **10.5** Create container log streaming endpoint
- [x] **10.6** Add build output streaming
- [ ] **10.7** Implement real-time deployment status updates
- [x] **10.8** Create HTMX SSE extension integration
- [ ] **10.9** Add reconnection handling with event replay
- [ ] **10.10** Implement client-side connection status indicator

### Phase 11: Resource Monitoring

- [x] **11.1** Create `internal/monitor/stats.go` for Docker stats collection
- [ ] **11.2** Implement periodic stats polling per container
- [ ] **11.3** Create metrics storage schema (time-series in PostgreSQL)
- [ ] **11.4** Implement metrics retention policy (7-day rolling)
- [ ] **11.5** Build CPU usage chart component (server-rendered SVG)
- [ ] **11.6** Build memory usage chart component
- [ ] **11.7** Create disk usage display
- [ ] **11.8** Implement container health status indicators
- [ ] **11.9** Add resource usage alerts (configurable thresholds)
- [ ] **11.10** Create dashboard overview with aggregate stats
- [ ] **11.11** Implement per-container detail view

### Phase 12: One-Click Services

- [x] **12.1** Create `internal/services/catalog.go` for template loading
- [x] **12.2** Define service template YAML schema
- [ ] **12.3** Implement template directory watching for hot-reload
- [x] **12.4** Create PostgreSQL service template
- [x] **12.5** Create Redis service template
- [ ] **12.6** Create MySQL/MariaDB service template
- [ ] **12.7** Create MongoDB service template
- [ ] **12.8** Create Plausible Analytics service template
- [ ] **12.9** Create PocketBase service template
- [x] **12.10** Build service catalog browser page
- [x] **12.11** Create service configuration wizard
- [x] **12.12** Implement service instance management in `internal/services/instance.go`
- [ ] **12.13** Add connection string exposure to linked projects
- [ ] **12.14** Create service backup/restore functionality

### Phase 13: Polish & Production Readiness

- [ ] **13.1** Implement rate limiting middleware
- [ ] **13.2** Add request validation and sanitization
- [ ] **13.3** Create comprehensive error handling
- [ ] **13.4** Implement audit logging
- [ ] **13.5** Add graceful shutdown handling
- [ ] **13.6** Create systemd service file `deploy/goforge.service`
- [ ] **13.7** Write Docker Compose deployment file `deploy/docker-compose.yml`
- [ ] **13.8** Create backup script for database
- [ ] **13.9** Implement restore functionality
- [ ] **13.10** Add Prometheus metrics endpoint (optional)
- [ ] **13.11** Create readiness endpoint `GET /health/ready`
- [ ] **13.12** Write integration tests for critical paths
- [ ] **13.13** Add E2E tests with Playwright
- [ ] **13.14** Create user documentation

---

## 4. Technical Architecture

### System Components & Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                         GoForge Server                          │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │   HTTP/HTMX  │  │   SSE Hub    │  │   Background Workers │  │
│  │   Handlers   │  │  (real-time) │  │   (builds, deploys)  │  │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬───────────┘  │
│         │                 │                      │              │
│  ┌──────▼─────────────────▼──────────────────────▼───────────┐  │
│  │                    Core Services                          │  │
│  │  ┌─────────┐ ┌──────────┐ ┌────────┐ ┌─────────────────┐  │  │
│  │  │ Project │ │   Git    │ │ Deploy │ │     Service     │  │  │
│  │  │ Service │ │ Service  │ │ Service│ │     Catalog     │  │  │
│  │  └────┬────┘ └────┬─────┘ └───┬────┘ └────────┬────────┘  │  │
│  └───────┼───────────┼───────────┼───────────────┼───────────┘  │
│          │           │           │               │              │
│  ┌───────▼───────────▼───────────▼───────────────▼───────────┐  │
│  │                     Infrastructure                        │  │
│  │  ┌──────────┐  ┌─────────────┐  ┌────────────────────┐   │  │
│  │  │  Docker  │  │   Traefik   │  │     PostgreSQL     │   │  │
│  │  │  Client  │  │   (proxy)   │  │     (database)     │   │  │
│  │  └──────────┘  └─────────────┘  └────────────────────┘   │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
       ┌──────────┐    ┌──────────┐    ┌──────────┐
       │  Docker  │    │ Traefik  │    │   Git    │
       │  Engine  │    │          │    │ Providers│
       └──────────┘    └──────────┘    └──────────┘
```

### Request Flow

```
Browser Request
      │
      ▼
┌─────────────┐
│   Traefik   │ ◄── SSL termination, routing
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  GoForge    │
│  HTTP       │
│  Router     │
└──────┬──────┘
       │
       ├──► Middleware Stack
       │    ├── Request ID
       │    ├── Logging
       │    ├── Recovery
       │    ├── Session
       │    ├── CSRF
       │    └── Auth
       │
       ▼
┌─────────────┐
│  Handler    │
└──────┬──────┘
       │
       ├──► Service Layer
       │    └── Business Logic
       │
       ├──► Repository Layer
       │    └── Database Queries
       │
       ▼
┌─────────────┐
│   templ     │
│  Render     │
└──────┬──────┘
       │
       ▼
  HTML Response
  (full page or HTMX partial)
```

### API Design

All routes return HTML fragments for HTMX or full pages for direct navigation.

```
Authentication:
  GET    /login                   Login page with GitHub OAuth button
  GET    /auth/github             Initiate GitHub OAuth
  GET    /auth/github/callback    GitHub OAuth callback
  POST   /auth/logout             Logout (clears session)
  GET    /auth/register           Local registration page (fallback)
  POST   /auth/register           Create local account

Dashboard:
  GET    /                        Dashboard (redirect if not auth'd)
  GET    /dashboard               Main dashboard view

Projects:
  GET    /projects                Project list (HTMX partial supported)
  POST   /projects                Create project
  GET    /projects/new            New project form
  GET    /projects/{id}           Project detail (overview tab)
  PUT    /projects/{id}           Update project settings
  DELETE /projects/{id}           Delete project
  GET    /projects/{id}/settings  Project settings tab
  
Environments:
  GET    /projects/{id}/envs                    Environment list
  POST   /projects/{id}/envs                    Create environment
  GET    /projects/{id}/envs/{env}              Environment detail
  DELETE /projects/{id}/envs/{env}              Delete environment
  GET    /projects/{id}/envs/{env}/variables    Variable editor
  PUT    /projects/{id}/envs/{env}/variables    Update variables
  POST   /projects/{id}/envs/{env}/variables    Add variable
  DELETE /projects/{id}/envs/{env}/variables/{key}  Delete variable

Deployments:
  GET    /projects/{id}/deployments             Deployment history
  POST   /projects/{id}/deployments             Trigger deployment
  GET    /deployments/{id}                      Deployment detail
  POST   /deployments/{id}/rollback             Rollback to this deployment
  GET    /deployments/{id}/logs                 SSE log stream

Git Sources:
  GET    /settings/git                          Git source list
  POST   /settings/git                          Add git source
  GET    /settings/git/{id}                     Git source detail
  PUT    /settings/git/{id}                     Update git source
  DELETE /settings/git/{id}                     Remove git source
  GET    /settings/git/{id}/repos               Repository browser (HTMX)
  GET    /settings/git/{id}/repos/{repo}/branches  Branch list (HTMX)

Services (One-Click):
  GET    /services                              Service catalog
  GET    /services/{template}                   Service template detail
  GET    /services/{template}/configure         Configuration wizard
  POST   /services                              Deploy service instance
  GET    /service-instances                     Running instances
  GET    /service-instances/{id}                Instance detail
  PUT    /service-instances/{id}                Update instance config
  DELETE /service-instances/{id}                Remove instance
  POST   /service-instances/{id}/restart        Restart service

Monitoring:
  GET    /containers                            Container list
  GET    /containers/{id}                       Container detail
  GET    /containers/{id}/stats                 SSE resource stream
  GET    /containers/{id}/logs                  SSE log stream
  POST   /containers/{id}/restart               Restart container
  POST   /containers/{id}/stop                  Stop container

Webhooks (External):
  POST   /webhooks/github/{project_id}          GitHub webhook receiver
  POST   /webhooks/gitlab/{project_id}          GitLab webhook receiver
  POST   /webhooks/gitea/{project_id}           Gitea webhook receiver

Health:
  GET    /health                                Liveness check
  GET    /health/ready                          Readiness check (DB, Docker)
```

### Database Schema

```sql
-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255), -- NULL for OAuth-only users
    github_id BIGINT UNIQUE,
    github_username VARCHAR(255),
    github_access_token TEXT, -- encrypted
    avatar_url TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_github_id ON users(github_id);

-- Sessions
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    last_active_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- Git Sources
CREATE TABLE git_sources (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('github', 'gitlab', 'gitea', 'ssh')),
    api_url TEXT, -- For self-hosted instances
    access_token TEXT, -- encrypted
    ssh_private_key TEXT, -- encrypted
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_git_sources_user_id ON git_sources(user_id);

-- Projects
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    git_source_id UUID REFERENCES git_sources(id) ON DELETE SET NULL,
    repository_url TEXT NOT NULL,
    repository_name VARCHAR(255), -- owner/repo format
    default_branch VARCHAR(255) DEFAULT 'main',
    dockerfile_path VARCHAR(255) DEFAULT 'Dockerfile',
    docker_compose_path VARCHAR(255), -- NULL if not using compose
    build_context VARCHAR(255) DEFAULT '.',
    build_args JSONB DEFAULT '{}',
    health_check_path VARCHAR(255) DEFAULT '/',
    health_check_interval INTEGER DEFAULT 30, -- seconds
    auto_deploy BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_projects_user_id ON projects(user_id);
CREATE INDEX idx_projects_git_source_id ON projects(git_source_id);

-- Environments
CREATE TABLE environments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(50) NOT NULL, -- dev, staging, prod, or custom
    domain VARCHAR(255),
    port INTEGER,
    branch VARCHAR(255), -- override project default
    replicas INTEGER DEFAULT 1,
    cpu_limit VARCHAR(20), -- e.g., "0.5", "2"
    memory_limit VARCHAR(20), -- e.g., "512m", "2g"
    auto_deploy BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(project_id, name)
);

CREATE INDEX idx_environments_project_id ON environments(project_id);
CREATE INDEX idx_environments_domain ON environments(domain);

-- Environment Variables
CREATE TABLE env_variables (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL, -- encrypted if is_secret
    is_secret BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(environment_id, key)
);

CREATE INDEX idx_env_variables_environment_id ON env_variables(environment_id);

-- Deployments
CREATE TABLE deployments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'cloning', 'building', 'deploying', 'running', 'failed', 'rolled_back', 'cancelled')),
    trigger_type VARCHAR(50) NOT NULL DEFAULT 'manual'
        CHECK (trigger_type IN ('manual', 'webhook', 'rollback')),
    commit_sha VARCHAR(40),
    commit_message TEXT,
    commit_author VARCHAR(255),
    branch VARCHAR(255),
    image_tag VARCHAR(255),
    error_message TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_deployments_environment_id ON deployments(environment_id);
CREATE INDEX idx_deployments_status ON deployments(status);
CREATE INDEX idx_deployments_created_at ON deployments(created_at DESC);

-- Deployment Logs
CREATE TABLE deployment_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    deployment_id UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ DEFAULT NOW(),
    level VARCHAR(20) DEFAULT 'info' CHECK (level IN ('debug', 'info', 'warn', 'error')),
    message TEXT NOT NULL,
    source VARCHAR(50) -- build, deploy, health_check, etc.
);

CREATE INDEX idx_deployment_logs_deployment_id ON deployment_logs(deployment_id);
CREATE INDEX idx_deployment_logs_timestamp ON deployment_logs(deployment_id, timestamp);

-- Containers
CREATE TABLE containers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    deployment_id UUID REFERENCES deployments(id) ON DELETE SET NULL,
    service_instance_id UUID REFERENCES service_instances(id) ON DELETE SET NULL,
    docker_id VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    image VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    ports JSONB DEFAULT '[]', -- [{host: 8080, container: 80}]
    labels JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_containers_deployment_id ON containers(deployment_id);
CREATE INDEX idx_containers_service_instance_id ON containers(service_instance_id);
CREATE INDEX idx_containers_docker_id ON containers(docker_id);

-- Service Instances (one-click services)
CREATE TABLE service_instances (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    template_name VARCHAR(100) NOT NULL, -- postgres, redis, etc.
    name VARCHAR(255) NOT NULL,
    version VARCHAR(50), -- specific version if chosen
    config JSONB DEFAULT '{}', -- template-specific configuration
    domain VARCHAR(255),
    status VARCHAR(50) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'starting', 'running', 'stopped', 'failed')),
    connection_info JSONB DEFAULT '{}', -- encrypted connection strings
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_service_instances_user_id ON service_instances(user_id);
CREATE INDEX idx_service_instances_template_name ON service_instances(template_name);

-- Container Metrics (time-series)
CREATE TABLE container_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    container_id UUID NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ DEFAULT NOW(),
    cpu_percent DECIMAL(5,2),
    memory_usage BIGINT, -- bytes
    memory_limit BIGINT, -- bytes
    memory_percent DECIMAL(5,2),
    network_rx BIGINT, -- bytes
    network_tx BIGINT, -- bytes
    block_read BIGINT, -- bytes
    block_write BIGINT -- bytes
);

CREATE INDEX idx_container_metrics_container_timestamp ON container_metrics(container_id, timestamp DESC);

-- Partitioning for metrics (optional, for better performance)
-- This would be set up separately based on PostgreSQL version

-- Audit Logs
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    resource_id UUID,
    details JSONB DEFAULT '{}',
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);

-- Cleanup function for old metrics
CREATE OR REPLACE FUNCTION cleanup_old_metrics() RETURNS void AS $$
BEGIN
    DELETE FROM container_metrics WHERE timestamp < NOW() - INTERVAL '7 days';
    DELETE FROM deployment_logs WHERE timestamp < NOW() - INTERVAL '30 days';
END;
$$ LANGUAGE plpgsql;
```

### File Structure

```
goforge/
├── cmd/
│   └── goforge/
│       └── main.go                    # Entry point, config loading, server start
├── internal/
│   ├── config/
│   │   └── config.go                  # Configuration struct and loading
│   ├── logger/
│   │   └── logger.go                  # Structured logging setup
│   ├── database/
│   │   ├── database.go                # PostgreSQL connection pool
│   │   ├── migrations.go              # Migration runner
│   │   ├── migrations/                # SQL migration files
│   │   │   ├── 001_create_users.up.sql
│   │   │   ├── 001_create_users.down.sql
│   │   │   └── ...
│   │   └── repositories/              # Data access layer
│   │       ├── user.go
│   │       ├── project.go
│   │       ├── environment.go
│   │       ├── deployment.go
│   │       └── ...
│   ├── models/
│   │   ├── user.go                    # User struct
│   │   ├── project.go                 # Project struct
│   │   ├── environment.go             # Environment struct
│   │   ├── deployment.go              # Deployment struct
│   │   └── ...
│   ├── auth/
│   │   ├── auth.go                    # Auth service interface
│   │   ├── password.go                # Argon2id hashing
│   │   ├── session.go                 # Session management
│   │   ├── csrf.go                    # CSRF middleware
│   │   ├── github.go                  # GitHub OAuth
│   │   └── middleware.go              # Auth middleware
│   ├── project/
│   │   ├── service.go                 # Project CRUD
│   │   ├── environment.go             # Environment management
│   │   └── variables.go               # Env var handling with encryption
│   ├── git/
│   │   ├── service.go                 # Git service interface
│   │   ├── github.go                  # GitHub API client
│   │   ├── gitlab.go                  # GitLab API client
│   │   ├── gitea.go                   # Gitea API client
│   │   ├── ssh.go                     # SSH operations
│   │   └── operations.go              # Clone, pull, etc.
│   ├── docker/
│   │   ├── client.go                  # Docker SDK wrapper
│   │   ├── build.go                   # Image building
│   │   ├── container.go               # Container lifecycle
│   │   ├── compose.go                 # Compose parsing
│   │   ├── logs.go                    # Log streaming
│   │   ├── network.go                 # Network management
│   │   └── volume.go                  # Volume management
│   ├── deploy/
│   │   ├── service.go                 # Deployment orchestration
│   │   ├── pipeline.go                # Build/deploy pipeline
│   │   ├── queue.go                   # Deployment queue
│   │   ├── state.go                   # State machine
│   │   └── rollback.go                # Rollback logic
│   ├── proxy/
│   │   ├── traefik.go                 # Traefik label generation
│   │   └── ssl.go                     # SSL/Let's Encrypt helpers
│   ├── services/
│   │   ├── catalog.go                 # Template loading and parsing
│   │   ├── instance.go                # Service instance management
│   │   └── schema.go                  # Template YAML schema
│   ├── monitor/
│   │   ├── stats.go                   # Docker stats collection
│   │   ├── metrics.go                 # Metrics storage
│   │   └── alerts.go                  # Alert thresholds
│   ├── sse/
│   │   ├── hub.go                     # SSE connection hub
│   │   ├── client.go                  # Client connection
│   │   └── events.go                  # Event types
│   ├── crypto/
│   │   └── encryption.go              # AES-256-GCM for secrets
│   └── web/
│       ├── router.go                  # HTTP routing setup
│       ├── middleware.go              # Common middleware
│       ├── handlers/
│       │   ├── auth.go                # Auth handlers
│       │   ├── dashboard.go           # Dashboard handler
│       │   ├── projects.go            # Project handlers
│       │   ├── environments.go        # Environment handlers
│       │   ├── deployments.go         # Deployment handlers
│       │   ├── services.go            # Service catalog handlers
│       │   ├── monitoring.go          # Monitoring handlers
│       │   ├── webhooks.go            # Webhook receivers
│       │   └── health.go              # Health check handlers
│       └── templates/
│           ├── layouts/
│           │   ├── base.templ         # Base HTML layout
│           │   └── auth.templ         # Auth page layout
│           ├── components/
│           │   ├── nav.templ          # Navigation
│           │   ├── sidebar.templ      # Sidebar
│           │   ├── toast.templ        # Notifications
│           │   ├── modal.templ        # Modal dialogs
│           │   ├── form.templ         # Form components
│           │   ├── table.templ        # Tables
│           │   ├── pagination.templ   # Pagination
│           │   ├── loading.templ      # Loading states
│           │   └── empty.templ        # Empty states
│           ├── pages/
│           │   ├── login.templ
│           │   ├── register.templ
│           │   ├── dashboard.templ
│           │   ├── projects/
│           │   │   ├── list.templ
│           │   │   ├── new.templ
│           │   │   ├── detail.templ
│           │   │   └── settings.templ
│           │   ├── environments/
│           │   │   ├── detail.templ
│           │   │   └── variables.templ
│           │   ├── deployments/
│           │   │   ├── list.templ
│           │   │   └── detail.templ
│           │   ├── services/
│           │   │   ├── catalog.templ
│           │   │   ├── configure.templ
│           │   │   └── instances.templ
│           │   └── errors/
│           │       ├── 404.templ
│           │       ├── 500.templ
│           │       └── 403.templ
│           └── partials/              # HTMX partial responses
│               ├── project_card.templ
│               ├── deployment_row.templ
│               └── ...
├── static/
│   ├── css/
│   │   └── output.css                 # Compiled TailwindCSS
│   ├── js/
│   │   ├── htmx.min.js                # HTMX library
│   │   └── htmx-sse.js                # SSE extension
│   └── img/
│       └── logo.svg
├── templates/                         # Service templates directory (configurable)
│   ├── postgres.yaml
│   ├── redis.yaml
│   ├── mysql.yaml
│   ├── mongodb.yaml
│   ├── plausible.yaml
│   └── pocketbase.yaml
├── deploy/
│   ├── docker-compose.yml             # Production deployment
│   ├── docker-compose.dev.yml         # Development
│   ├── goforge.service                 # systemd service
│   └── traefik/
│       └── traefik.yml                # Traefik base config
├── scripts/
│   ├── build.sh                       # Build binary
│   ├── tailwind.sh                    # Compile CSS
│   └── dev.sh                         # Development server
├── go.mod
├── go.sum
├── tailwind.config.js
├── Makefile
├── .env.example
├── .gitignore
└── README.md
```

---

## 5. Service Template Schema

Service templates are YAML files loaded from a configurable directory.

```yaml
# templates/postgres.yaml
name: postgresql
display_name: PostgreSQL
description: The world's most advanced open source relational database
icon: postgres.svg
category: database

versions:
  - "16"
  - "15"
  - "14"
default_version: "16"

config:
  - name: database
    label: Database Name
    type: string
    default: "app"
    required: true
  - name: username
    label: Username
    type: string
    default: "postgres"
    required: true
  - name: password
    label: Password
    type: password
    generate: true
    length: 32
    required: true
  - name: max_connections
    label: Max Connections
    type: number
    default: 100
    min: 10
    max: 1000

resources:
  cpu: "0.5"
  memory: "512m"

docker:
  image: "postgres:{{ .Version }}"
  ports:
    - container: 5432
      protocol: tcp
  environment:
    POSTGRES_DB: "{{ .Config.database }}"
    POSTGRES_USER: "{{ .Config.username }}"
    POSTGRES_PASSWORD: "{{ .Config.password }}"
  volumes:
    - name: data
      path: /var/lib/postgresql/data
  health_check:
    test: ["CMD-SHELL", "pg_isready -U {{ .Config.username }}"]
    interval: 10s
    timeout: 5s
    retries: 5

connection:
  internal: "postgres://{{ .Config.username }}:{{ .Config.password }}@{{ .ContainerName }}:5432/{{ .Config.database }}"
  external: "postgres://{{ .Config.username }}:{{ .Config.password }}@{{ .Domain }}:{{ .ExternalPort }}/{{ .Config.database }}"

backup:
  command: "pg_dump -U {{ .Config.username }} {{ .Config.database }}"
  extension: "sql"
```

---

## 6. Implementation Details

### Configuration

```go
// internal/config/config.go
type Config struct {
    // Server
    Host            string        `env:"GOFORGE_HOST" default:"0.0.0.0"`
    Port            int           `env:"GOFORGE_PORT" default:"8080"`
    BaseURL         string        `env:"GOFORGE_BASE_URL" default:"http://localhost:8080"`
    
    // Database
    DatabaseURL     string        `env:"DATABASE_URL" required:"true"`
    
    // Security
    EncryptionKey   string        `env:"ENCRYPTION_KEY" required:"true"` // 32 bytes, base64
    SessionSecret   string        `env:"SESSION_SECRET" required:"true"`
    CSRFSecret      string        `env:"CSRF_SECRET" required:"true"`
    
    // GitHub OAuth
    GitHubClientID     string     `env:"GITHUB_CLIENT_ID"`
    GitHubClientSecret string     `env:"GITHUB_CLIENT_SECRET"`
    
    // Docker
    DockerHost      string        `env:"DOCKER_HOST" default:"unix:///var/run/docker.sock"`
    
    // Traefik
    TraefikNetwork  string        `env:"TRAEFIK_NETWORK" default:"traefik"`
    AcmeEmail       string        `env:"ACME_EMAIL"` // For Let's Encrypt
    
    // Service Templates
    TemplatesDir    string        `env:"TEMPLATES_DIR" default:"./templates"`
    
    // Limits
    MaxConcurrentBuilds int       `env:"MAX_CONCURRENT_BUILDS" default:"2"`
    BuildTimeout        time.Duration `env:"BUILD_TIMEOUT" default:"15m"`
    
    // Features
    EnableMetrics   bool          `env:"ENABLE_METRICS" default:"true"`
    MetricsRetention time.Duration `env:"METRICS_RETENTION" default:"168h"` // 7 days
}
```

### Security Implementation

```
Password Hashing (Argon2id):
- Time: 1 iteration
- Memory: 64MB
- Parallelism: 4 threads
- Salt: 16 bytes (random)
- Key length: 32 bytes

Encryption (AES-256-GCM):
- Key: 32 bytes from ENCRYPTION_KEY
- Nonce: 12 bytes (random per encryption)
- Used for: Git tokens, SSH keys, secret env vars

Session Tokens:
- 32 bytes random
- Stored as SHA-256 hash in database
- Cookie: HTTP-only, Secure, SameSite=Lax
- Default TTL: 24 hours
- "Remember me" TTL: 30 days

CSRF:
- Token per session
- Validated via header or form field
- Required on all state-changing requests
```

### Rate Limiting

```
Endpoint Groups:
- Authentication (/auth/*): 5 req/min per IP
- Webhooks (/webhooks/*): 60 req/min per source
- API (all other): 100 req/min per user
- SSE connections: 10 concurrent per user

Implementation:
- Token bucket algorithm
- Redis-backed for distributed (future)
- In-memory for single server (v1)
```

### Docker Labels for Traefik

```go
// Generated labels for a deployment
labels := map[string]string{
    "traefik.enable": "true",
    "traefik.http.routers.{name}.rule": "Host(`{domain}`)",
    "traefik.http.routers.{name}.entrypoints": "websecure",
    "traefik.http.routers.{name}.tls.certresolver": "letsencrypt",
    "traefik.http.services.{name}.loadbalancer.server.port": "{port}",
    "traefik.docker.network": "{traefik_network}",
    // GoForge metadata
    "goforge.managed": "true",
    "goforge.project.id": "{project_id}",
    "goforge.environment.id": "{environment_id}",
    "goforge.deployment.id": "{deployment_id}",
}
```

---

## 7. Risk Mitigation

### Failure Modes & Recovery

| Failure | Detection | Mitigation |
|---------|-----------|------------|
| Docker daemon unavailable | Health check fails | Clear error, prevent deployments, retry connection |
| Build timeout (>15min) | Context deadline | Kill build, mark failed, cleanup resources |
| Git clone failure | Non-zero exit | Retry 3x with backoff, detailed error message |
| SSL cert failure | ACME error | Log error, continue without SSL, alert user |
| Database unavailable | Connection error | Exponential backoff retry, graceful degradation |
| Container OOM | Docker event | Restart with policy, alert user, suggest limits |
| Disk full | Build fails | Prune old images, alert user |

### Deployment Safety

1. **Blue-Green Strategy**
   - New container starts alongside old
   - Health check (3 successful checks, 10s interval)
   - Traffic switch via Traefik labels
   - Old container retained 5 minutes

2. **Rollback**
   - Last 5 deployment images retained
   - One-click rollback in UI
   - Automatic rollback if health check fails (configurable)

3. **Resource Limits**
   - Default CPU: 0.5 cores
   - Default Memory: 512MB
   - Configurable per environment

### Data Safety

1. **Encryption at Rest**
   - All secrets encrypted with AES-256-GCM
   - Encryption key from environment (not in DB)

2. **Backup Strategy**
   - PostgreSQL: pg_dump script provided
   - Service volumes: User responsibility (documented)

### Monitoring

```
Health Endpoints:
- GET /health - Returns 200 if server running
- GET /health/ready - Returns 200 if DB + Docker connected

Logs:
- Structured JSON format
- Request ID tracing
- Log levels: debug, info, warn, error

Metrics (optional):
- GET /metrics - Prometheus format
- request_duration_seconds
- deployment_total (by status)
- container_count
- build_duration_seconds
```

---

## 8. Development Workflow

### Prerequisites

```bash
# Required
- Go 1.25+
- Docker Engine
- PostgreSQL 15+
- templ CLI
- TailwindCSS standalone CLI

# Optional
- air (hot reload)
- golangci-lint
```

### Makefile Targets

```makefile
.PHONY: build dev css templ migrate test lint

# Build production binary
build: css templ
	go build -o bin/goforge ./cmd/goforge

# Development with hot reload
dev:
	air

# Compile TailwindCSS
css:
	./scripts/tailwind.sh

# Generate templ files
templ:
	templ generate

# Run migrations
migrate:
	go run ./cmd/goforge migrate up

# Run tests
test:
	go test -v -race ./...

# Lint
lint:
	golangci-lint run
```

### Environment Setup

```bash
# .env.example
DATABASE_URL=postgres://goforge:password@localhost:5432/goforge?sslmode=disable
ENCRYPTION_KEY=base64-encoded-32-byte-key
SESSION_SECRET=random-secret-for-sessions
CSRF_SECRET=random-secret-for-csrf
GITHUB_CLIENT_ID=your-github-oauth-client-id
GITHUB_CLIENT_SECRET=your-github-oauth-client-secret
GOFORGE_BASE_URL=http://localhost:8080
TRAEFIK_NETWORK=traefik
ACME_EMAIL=admin@example.com
TEMPLATES_DIR=./templates
```

---

## Appendix: Quick Reference

### Key Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Reverse Proxy | Traefik | Docker-native, automatic discovery |
| Database | PostgreSQL | Robust, scalable from day one |
| Auth | GitHub OAuth | Primary user flow, simpler UX |
| Templates | External directory | User customization without rebuild |
| Deployment | Single server | Reduced complexity for v1 |

### Critical Dependencies

```
github.com/go-chi/chi/v5        # Router
github.com/a-h/templ            # Templating  
github.com/lib/pq               # PostgreSQL
github.com/docker/docker        # Docker SDK
github.com/golang-migrate/migrate/v4  # Migrations
golang.org/x/crypto             # Crypto primitives
github.com/google/go-github/v53 # GitHub API
```

### Estimated Timeline

| Phase | Duration | Milestone |
|-------|----------|-----------|
| 1-2 | 1 week | Foundation + Database |
| 3-4 | 1 week | Auth + Core UI |
| 5-6 | 1.5 weeks | Projects + Git |
| 7-8 | 2 weeks | Docker + Deployments |
| 9-10 | 1.5 weeks | Traefik + Real-time |
| 11-12 | 1.5 weeks | Monitoring + Services |
| 13 | 1 week | Polish + Production |

**Total: ~10 weeks for v1**
