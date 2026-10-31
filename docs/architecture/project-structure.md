# Project Structure

This page documents the complete directory layout of the GoForge codebase.

## Top-Level Structure

```
goforge/
├── cmd/goforge/           # Application entry point
│   └── main.go            # CLI setup (serve, migrate, backup run|list|restore)
├── cmd/goforge-mcp/       # MCP stdio fallback (the server also serves /mcp)
│   └── main.go            # Calls /api/v1 over HTTPS with a scoped token
├── internal/              # Private application packages
│   ├── apiv1/             # Bearer-token JSON API under /api/v1
│   ├── auth/              # Authentication and authorization
│   ├── backup/            # Nightly S3 backups and backup runs
│   ├── config/            # Configuration loading
│   ├── database/          # Database connection and repositories
│   ├── deploy/            # Deployment pipeline
│   ├── docker/            # Docker SDK wrapper
│   ├── git/               # Git provider integrations
│   ├── logger/            # Structured logging
│   ├── mcphttp/           # /mcp: MCP over streamable HTTP for Profile tokens
│   ├── mcpserver/         # MCP tools (shared by /mcp and cmd/goforge-mcp)
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
| `main.go` | Root command, `serve` subcommand (HTTP server and nightly backup scheduler), `migrate` subcommands (up, down, status, create), `backup run` (one backup now, `trigger=manual`); `backup list` and `backup restore` are in `backup_restore.go` |

### `cmd/goforge-mcp/`

The MCP stdio fallback, for clients that cannot use `/mcp` over HTTP. Built with `make build-mcp` or the Dockerfile `mcp` target. Point it at the public HTTPS URL. See [MCP Server](../guides/mcp.md#stdio-fallback).

| File | Purpose |
|------|---------|
| `main.go` | Loads config, then serves MCP over stdio. Holds no Docker socket and no database connection; every call goes to `/api/v1` with a scoped bearer token |

### `internal/apiv1/`

JSON API for automation under `/api/v1`. Bearer token only, outside the CSRF group. See [API v1](../reference/api-v1.md).

| File | Purpose |
|------|---------|
| `handler.go` | Route definitions |
| `auth.go` | Bearer token auth and scope checks; session cookies are ignored |
| `gate.go` | Dry-run default, `approval_ref`, and audit-before-change for mutations |
| `ownership.go` | Ownership checks that answer like the UI routes (`404` for another user's resources) |
| `reads.go` | Read routes; secrets are masked |
| `mutations.go` | Gated write routes |
| `backups.go` | `GET /api/v1/backups` (admin) |
| `dto.go` | Response types |
| `respond.go` | JSON responses and stable error codes |
| `deps.go` | Package doc and dependencies |

### `internal/backup/`

Nightly, backup-only copies of GoForge state to S3-compatible storage. See [Backup & Restore](../deployment/backup.md).

| File | Purpose |
|------|---------|
| `doc.go` | Package overview and object layout |
| `setup.go` | Builds the runner from config; `ErrDisabled` when no bucket is set or backups are turned off |
| `settings.go` | Saved settings vs `BACKUP_*` (the saved row wins), first-start import, form save and Test connection |
| `manager.go` | Owns the schedule loop in `goforge serve` and swaps it when settings are saved, without stopping a run in progress |
| `scheduler.go` | Runs the runner at each scheduled time |
| `runner.go` | One run: advisory lock, `backup_runs` row, targets, manifest, retention |
| `target.go` | Target and artifact types |
| `appdb.go` | GoForge database dump with the local `pg_dump` |
| `instances.go` | Postgres, Gitea, and Vaultwarden service targets via `docker exec` |
| `store.go` | S3 object store |
| `layout.go` | Run keys and object layout |
| `retention.go` | Which old runs to prune |
| `redact.go` | Removes secret values from errors and logs |
| `status.go` | Last-run status for Settings and the API |

### `internal/auth/`

Authentication, session management, and security middleware.

| File | Purpose |
|------|---------|
| `auth.go` | `Authenticator` interface, `LocalAuthenticator` for email/password login |
| `session.go` | `SessionManager` -- create, validate, destroy sessions with SHA-256 token hashing |
| `middleware.go` | Auth middleware (session loading) and `RequireAuth` middleware |
| `csrf.go` | CSRF middleware: HMAC-SHA256 tokens keyed by `CSRF_SECRET`, session-bound when signed in |
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
| `api_token.go` | APIToken | Create, get by ID or token hash, list by user, delete, record last use |
| `backup_run.go` | BackupRun | Start, finish, list, latest succeeded, mark interrupted runs, advisory lock |

### `internal/deploy/`

Deployment pipeline orchestration.

| File | Purpose |
|------|---------|
| `service.go` | `Service` -- deployment orchestration coordinator |
| `pipeline.go` | `Pipeline` -- clone, build, deploy sequence |
| `queue.go` | `Worker` -- PostgreSQL-backed persistent deployment queue with polling |
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

### `internal/mcphttp/`

`/mcp` on `goforge serve`. See [MCP Server](../guides/mcp.md).

| File | Purpose |
|------|---------|
| `mcphttp.go` | `http.CrossOriginProtection`, bearer auth (`auth.AuthenticateBearer`; `full` refused), and the go-sdk streamable HTTP handler (stateless, JSON). Builds one `mcpserver` per request with the caller's token; write tools only with `write` |
| `transport.go` | In-process `http.RoundTripper` that serves `mcpserver`'s `/api/v1` calls with the API handler, keeping the caller's peer address and proxy headers for the audit IP |

### `internal/mcpserver/`

The MCP tools, shared by `/mcp` (`internal/mcphttp`) and `cmd/goforge-mcp`. See [MCP Server](../guides/mcp.md).

| File | Purpose |
|------|---------|
| `config.go` | Package doc and configuration (API URL, token, `GOFORGE_MCP_ALLOW_WRITES`, `WriteRefusal` for `/mcp` read tokens) |
| `server.go` | MCP server setup, tool registration, and the write refusal middleware |
| `client.go` | HTTP client for `/api/v1` |
| `reads.go` | Read tools |
| `writes.go` | Gated write tools, registered only when writes are allowed |

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
│   └── base.templ         # Base(title, csrfToken), AuthLayout, AppLayout
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
    ├── ui/                # UI kit (package ui), see docs/reference/ui-kit.md
    ├── sidebar.templ      # Desktop sidebar and mobile navigation Sheet
    ├── toast.templ        # Toast notifications
    ├── status.templ       # StatusBadge wrappers over ui.Badge
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
