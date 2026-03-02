# Project Structure

This page documents the complete directory layout of the GoForge codebase.

## Top-Level Structure

```
goforge/
├── cmd/goforge/           # Application entry point
│   └── main.go            # CLI setup (serve, migrate commands)
├── internal/              # Private application packages
│   ├── auth/              # Authentication and authorization
│   ├── config/            # Configuration loading
│   ├── database/          # Database connection and repositories
│   ├── deploy/            # Deployment pipeline
│   ├── docker/            # Docker SDK wrapper
│   ├── domain/            # Custom domain management
│   ├── git/               # Git provider integrations
│   ├── logger/            # Structured logging
│   ├── models/            # Domain models
│   ├── monitor/           # Container metrics
│   ├── pagination/        # Pagination utilities
│   ├── project/           # Project service layer
│   ├── proxy/             # Traefik integration
│   ├── secrets/           # Encryption services
│   ├── services/          # One-click service catalog
│   ├── sse/               # Server-Sent Events
│   ├── web/               # HTTP router and handlers
│   └── webhook/           # Git webhook handling
├── static/                # Static assets
│   ├── css/               # TailwindCSS input/output
│   └── js/                # HTMX and SSE extension
├── templates/             # Service YAML templates
├── deploy/                # Deployment artifacts
│   └── goforge.service    # Systemd service file
├── docs/                  # MkDocs documentation
├── scripts/               # Build scripts
└── .github/workflows/     # CI/CD pipelines
```

## Package Details

### `cmd/goforge/`

The application entry point using Cobra for CLI structure.

| File | Purpose |
|------|---------|
| `main.go` | Root command, `serve` subcommand (HTTP server), `migrate` subcommands (up, down, status, create) |

### `internal/auth/`

Authentication, session management, and security middleware.

| File | Purpose |
|------|---------|
| `auth.go` | `Authenticator` interface, `LocalAuthenticator` for email/password login |
| `session.go` | `SessionManager` -- create, validate, destroy sessions with SHA-256 token hashing |
| `middleware.go` | Auth middleware (session loading) and `RequireAuth` middleware |
| `csrf.go` | CSRF protection middleware using double-submit cookie pattern |
| `github.go` | `GitHubProvider` -- OAuth flow, user info retrieval |
| `cookie.go` | Session and CSRF cookie helpers |
| `password.go` | Argon2id password hashing and verification |

### `internal/config/`

Environment-based configuration with development mode support.

| File | Purpose |
|------|---------|
| `config.go` | `Config` struct, `Load()` from environment, validation, defaults |

### `internal/database/`

Database connectivity, migrations, and data access.

| File | Purpose |
|------|---------|
| `database.go` | Connection setup with retry logic and connection pooling |
| `migrations.go` | Migration runner using golang-migrate (up, down, status, create) |
| `repositories/` | Repository implementations for all entities |

#### `internal/database/repositories/`

| File | Entity | Key Operations |
|------|--------|---------------|
| `user.go` | User | CRUD, find by email/GitHub ID, token encryption |
| `session.go` | Session | Create, get by token hash, delete, cleanup expired |
| `project.go` | Project | CRUD, list by user, count |
| `environment.go` | Environment | CRUD, list by project, get default |
| `env_variable.go` | EnvVariable | CRUD, list by environment, bulk operations |
| `deployment.go` | Deployment, DeploymentLog | CRUD, list by project/environment, status updates |
| `container.go` | Container | CRUD, find by deployment/Docker ID |
| `git_source.go` | GitSource | CRUD, find by project, token encryption |
| `service_instance.go` | ServiceInstance | CRUD, list by user |
| `audit_log.go` | AuditLog | Create, list with pagination and filters |

### `internal/deploy/`

Deployment pipeline orchestration.

| File | Purpose |
|------|---------|
| `service.go` | `Service` -- deployment orchestration coordinator |
| `pipeline.go` | `Pipeline` -- clone, build, deploy sequence |
| `queue.go` | `Worker` -- buffered deployment queue with timeout |
| `state.go` | State machine for deployment status transitions |
| `bluegreen.go` | `BlueGreenDeployer` -- zero-downtime deployment strategy |
| `health.go` | `HealthChecker` -- container health verification |
| `rollback.go` | `RollbackManager` -- deployment rollback operations |
| `cleanup.go` | Old deployment and image cleanup |

### `internal/docker/`

Docker Engine SDK wrapper.

| File | Purpose |
|------|---------|
| `client.go` | Docker client initialization, `dockerAPI` interface, image management |
| `build.go` | Docker image building from Dockerfile |
| `container.go` | Container create, start, stop, remove |
| `logs.go` | Container log streaming |
| `compose.go` | Docker Compose YAML parser |
| `prune.go` | System prune (images, containers) |

### `internal/git/`

Git provider integrations.

| File | Purpose |
|------|---------|
| `service.go` | `Service` -- provider registry and routing |
| `operations.go` | Git clone and pull via go-git |
| `github.go` | GitHub API (repos, branches, tags, file content) |
| `gitlab.go` | GitLab API (repos, branches, tags) |
| `gitea.go` | Gitea API (repos, branches, tags) |
| `ssh.go` | Ed25519 SSH key generation, SSH auth |

### `internal/models/`

Domain model definitions.

| File | Models |
|------|--------|
| `user.go` | `User` |
| `project.go` | `Project` |
| `deployment.go` | `Deployment`, `DeploymentStatus`, `DeploymentLog` |
| `environment.go` | `Environment` |
| `session.go` | `Session` |
| `container.go` | `Container` |
| `git_source.go` | `GitSource`, `GitSourceType` |
| `env_variable.go` | `EnvVariable` |
| `service_instance.go` | `ServiceInstance` |
| `audit_log.go` | `AuditLog` |
| `stats.go` | `SystemStats`, `ContainerMetrics` |
| `custom_domain.go` | `CustomDomain`, `SSLStatus` |

### `internal/web/`

HTTP layer.

| File | Purpose |
|------|---------|
| `router.go` | Chi router setup with all routes and middleware |
| `middleware/ratelimit.go` | Token bucket rate limiter |
| `handlers/auth.go` | Login, register, logout, GitHub OAuth callback |
| `handlers/dashboard.go` | Dashboard page with stats |
| `handlers/projects.go` | Project CRUD handlers |
| `handlers/environments.go` | Environment management handlers |
| `handlers/deployments.go` | Deployment trigger, status, logs |
| `handlers/services.go` | One-click service management |
| `handlers/settings.go` | User settings page |
| `handlers/git.go` | Git source configuration, repo browser |
| `handlers/sse.go` | SSE endpoints for logs, stats, deployment events |
| `handlers/health.go` | Health check endpoint |

### `internal/web/templates/`

templ-based HTML templates.

```
templates/
├── layouts/
│   └── base.templ         # Base HTML layout with sidebar
├── pages/
│   ├── dashboard.templ    # Dashboard page
│   ├── login.templ        # Login form
│   ├── register.templ     # Registration form
│   ├── projects/          # Project list and detail views
│   ├── deployments/       # Deployment views
│   ├── services/          # Service catalog and instances
│   ├── settings/          # User settings
│   └── errors/            # Error pages (404, 500)
└── components/
    ├── sidebar.templ      # Navigation sidebar
    ├── nav.templ          # Top navigation
    ├── form.templ         # Reusable form components
    ├── modal.templ        # Modal dialogs
    ├── toast.templ        # Toast notifications
    ├── table.templ        # Data tables
    ├── pagination.templ   # Pagination controls
    ├── stats.templ        # Stats cards
    ├── variables.templ    # Environment variable editor
    ├── environment.templ  # Environment components
    ├── git_browser.templ  # Repository browser
    ├── git_components.templ # Git-related UI
    ├── deployments.templ  # Deployment status components
    ├── empty.templ        # Empty state displays
    └── loading.templ      # Loading indicators
```

## Configuration Files

| File | Purpose |
|------|---------|
| `go.mod` | Go module definition and dependencies |
| `Makefile` | Build, test, lint, and development commands |
| `Dockerfile` | Multi-stage production Docker build |
| `docker-compose.yml` | Full stack deployment configuration |
| `.golangci.yml` | Linter configuration (30+ linters) |
| `.air.toml` | Hot reload configuration for development |
| `.env.example` | Environment variable template |
| `.editorconfig` | Editor formatting settings |
| `tailwind.config.js` | TailwindCSS configuration |
| `tools.go` | Go tool dependency pinning |
