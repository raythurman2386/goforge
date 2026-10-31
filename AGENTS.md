# AGENTS.md - AI Coding Agent Guidelines

This document provides guidelines for AI coding agents working in the GoForge codebase.
Humans start at [CONTRIBUTING.md](CONTRIBUTING.md), which holds the house rules, test
types, migration and template rules, and the PR format. Agents follow it too.
`GEMINI.md` points here; edit this file, not that one.

## Project Overview

GoForge is a lightweight, self-hosted PaaS built in Go with HTMX + templ. It provides
git-to-deploy workflows, one-click services, automatic SSL via Traefik, and real-time
monitoring. Tech stack: Go 1.26+, PostgreSQL, Docker, HTMX 2, templ, TailwindCSS.

## Build, Lint, and Test Commands

```bash
# Build
make build                          # Build production binary (runs templ + css first)
make build-mcp                      # Build bin/goforge-mcp
make dev                            # Run with hot reload via air

# Format and Lint
make fmt                            # Format code with gofmt -s -w
make fmt-check                      # Check formatting (CI mode, exits non-zero if unformatted)
make vet                            # Run go vet
make lint                           # Run golangci-lint (30+ linters configured)
make check                          # Run all checks: fmt-check, vet, lint, test
make ci                             # deps, templ, css-check, then the make check steps

# Testing
make test                           # Run all tests with race detector and coverage
make test-short                     # Run tests without race detector (faster)
make test-coverage                  # Generate HTML coverage report

# Run a single test
go test -v -run TestFunctionName ./internal/auth/...
go test -v -run TestDeployment ./internal/deploy/...
go test -v -run "TestName/subtest" ./path/to/package/...

# Run tests in a specific package
go test -v ./internal/docker/...

# Integration tests (//go:build integration): need Docker, create and remove
# throwaway containers. Never run them against prod containers or databases.
go test -tags integration -run TestGiteaPostStartBootstrap ./internal/services/
DATABASE_URL=postgres://... go test -tags integration -run Integration ./internal/backup/
# Restore: own MinIO + postgres:15-alpine and 17-alpine; needs pg_restore/psql 17 on PATH
go test -tags integration -run TestIntegration_Restore ./internal/backup/

# Docs (run when docs/ or mkdocs.yml changes; deps pinned in requirements-docs.txt)
pip install -r requirements-docs.txt && mkdocs build --strict
docker build -f Dockerfile.docs -t goforge-docs .   # static site image, nginx on :80

# Code generation
make templ                          # Generate templ templates (*_templ.go files)
make css                            # Compile TailwindCSS v3.4.17 (standalone CLI if it is
                                    # v3.4.17, else npx tailwindcss@3.4.17); fails otherwise
make css-check                      # Rebuild output.css, then scripts/class_check.py: fails on
                                    # class tokens (class="...", class={ ... }, *Class() helpers,
                                    # ui.Class()/"class" attrs, app.js and ui.js class
                                    # literals) missing from output.css
                                    # (CI css job)

# Database
make migrate                        # Run migrations up
make migrate-down                   # Rollback last migration
make migrate-create name=xyz        # Create new migration
```

## Code Style Guidelines

### Formatting
- Use `gofmt` (enforced by CI) - tabs for indentation in Go files
- Run `make fmt` before committing
- Line length: no hard limit, but keep readable (~100-120 chars)

### Import Organization
Imports must be grouped in this order, separated by blank lines:
```go
import (
    // Standard library
    "context"
    "fmt"
    "net/http"

    // Third-party packages
    "github.com/go-chi/chi/v5"
    "github.com/lib/pq"

    // Local packages (github.com/raythurman2386/goforge/...)
    "github.com/raythurman2386/goforge/internal/config"
    "github.com/raythurman2386/goforge/internal/models"
)
```

### Naming Conventions
- **Packages**: lowercase, single word preferred (`auth`, `deploy`, `docker`)
- **Files**: lowercase with underscores (`deployment_service.go`, `github_client.go`)
- **Interfaces**: describe behavior, often `-er` suffix (`Deployer`, `Builder`, `Repository`)
- **Structs**: noun, PascalCase (`Deployment`, `GitSource`, `Container`)
- **Functions/Methods**: verb or verb phrase (`CreateProject`, `Deploy`, `BuildImage`)
- **Variables**: camelCase, descriptive (`projectID`, `deploymentStatus`, `errNotFound`)
- **Constants**: PascalCase for exported, camelCase for unexported
- **Errors**: prefix with `Err` (`ErrNotFound`, `ErrUnauthorized`, `ErrBuildFailed`)

### Error Handling
```go
// Always handle errors explicitly - never ignore
result, err := doSomething()
if err != nil {
    return fmt.Errorf("failed to do something: %w", err)  // Wrap with context
}

// Use sentinel errors for expected conditions
var ErrNotFound = errors.New("not found")

// Check sentinel errors with errors.Is
if errors.Is(err, ErrNotFound) {
    // handle not found
}

// Error messages: lowercase, no punctuation at end
return errors.New("connection failed")  // correct
return errors.New("Connection failed.") // wrong
```

### Templates and UI
- Pages and shared components use the kit in `internal/web/templates/components/ui`
  (`ui.Button`, `ui.Card`, `ui.Input`, `ui.Dialog`, ...). Reference:
  `docs/reference/ui-kit.md`. Do not hand-write `class="btn…"`, `class="card…"`,
  `class="input"`, `class="select"`, or `class="label"`, inline `onclick=`, or
  `confirm(`. `TestPagesUseKit` (`components/sweep_gate_test.go`) enforces it.
- Layouts: `layouts.Base(title, csrfToken)`, `layouts.AuthLayout(title, csrfToken)`,
  `layouts.AppLayout(title, csrfToken)`.
- Behavior goes in the delegated runtimes: `static/js/ui.js` (kit, `data-ui*`
  attributes) or `static/js/app.js` (toasts, htmx error toasts, active nav).
  Both are class sources for `tailwind.config.js` `content` and
  `scripts/class_check.py`; keep every class a complete literal.
- Dark mode is OS-only (`prefers-color-scheme`); there is no toggle.

### Context
- First parameter for functions doing I/O or cancellable work
- Pass through, don't store in structs
```go
func (s *Service) Deploy(ctx context.Context, projectID string) error {
    // Use ctx for cancellation, timeouts, request-scoped values
}
```

### Structs and Types
```go
// Group related fields, add comments for non-obvious fields
type Deployment struct {
    ID            string
    EnvironmentID string
    Status        DeploymentStatus
    CommitSHA     string
    StartedAt     time.Time
    FinishedAt    *time.Time  // nil if still running
}

// Use custom types for enums
type DeploymentStatus string

const (
    StatusPending   DeploymentStatus = "pending"
    StatusBuilding  DeploymentStatus = "building"
    StatusDeploying DeploymentStatus = "deploying"
    StatusRunning   DeploymentStatus = "running"
    StatusFailed    DeploymentStatus = "failed"
)
```

### Testing
- Place tests in `*_test.go` next to the code
- Use table-driven tests for multiple cases
- Name test functions `TestXxx` or `TestXxx_Scenario`
```go
func TestDeploymentService_CreateDeployment(t *testing.T) {
    tests := []struct {
        name    string
        input   CreateDeploymentInput
        wantErr bool
    }{
        {"valid deployment", CreateDeploymentInput{...}, false},
        {"missing project", CreateDeploymentInput{}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test logic
        })
    }
}
```

## Project Structure

```
cmd/goforge/              # Entry point (main.go): serve, migrate, backup run|list|restore
cmd/goforge-mcp/          # MCP server over stdio; calls /api/v1 with a scoped token
internal/
  apiv1/                 # Bearer-token JSON API under /api/v1 (scopes, gated writes)
  auth/                  # Authentication (GitHub OAuth, sessions, CSRF)
  backup/                # Nightly S3 backups, `goforge backup run`, restore points, `goforge backup restore` (app DB only)
  config/                # Configuration loading from env
  database/              # PostgreSQL connection, migrations, repositories
  deploy/                # Deployment pipeline and orchestration
  docker/                # Docker SDK wrapper (build, container, logs)
  git/                   # Git providers (GitHub, GitLab, Gitea)
  logger/                # Structured logging
  mcpserver/             # MCP tools used by cmd/goforge-mcp
  models/                # Domain models (structs)
  monitor/               # Resource monitoring (CPU, memory, disk)
  pagination/            # Pagination helpers
  project/               # Project and environment management
  proxy/                 # Traefik integration
  secrets/               # AES-GCM encryption at rest (ENCRYPTION_KEY)
  services/              # One-click service catalog
  sse/                   # Server-Sent Events hub
  webhook/               # Git webhook handling
  web/
    handlers/            # HTTP handlers
    templates/
      layouts/           # Base templates (base.templ)
      components/        # Shared components (sidebar.templ, toast.templ, ...)
        ui/              # UI kit: pages use these (docs/reference/ui-kit.md)
      pages/             # Page templates
static/                  # CSS (input.css tokens), JS (htmx, app.js shell, ui.js kit runtime)
templates/               # Service YAML templates (postgres.yaml, redis.yaml)
```

## Key Patterns

### Repository Pattern (database access)
```go
type ProjectRepository interface {
    Create(ctx context.Context, p *models.Project) error
    GetByID(ctx context.Context, id string) (*models.Project, error)
    List(ctx context.Context, userID string) ([]*models.Project, error)
}
```

### Service Layer (business logic)
```go
type DeploymentService struct {
    repo     DeploymentRepository
    docker   DockerClient
    git      GitService
}
```

### HTTP Handlers (thin, delegate to services)
```go
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
    // Parse input, call service, render response
}
```

## Linter Notes

The project uses golangci-lint with 30+ linters. Key ones to be aware of:
- `errcheck`: All errors must be handled
- `bodyclose`: HTTP response bodies must be closed
- `sqlclosecheck`: sql.Rows must be closed
- `contextcheck`: context.Context must be passed correctly
- `gosec`: Security issues flagged
- `errorlint`: Use `errors.Is`/`errors.As` for error comparison

Generated `*_templ.go` files are excluded from linting.

## Verifying changes

- `make check` must pass before you push.
- For UI or HTTP-visible behavior, drive the change with the
  `.cursor/skills/verify-goforge/` skill (`scripts/verify.sh launch`, then the
  feature file). Never commit its proofs.
- Add a line to `CHANGELOG.md` under `## [Unreleased]` for user-visible changes,
  and update the matching page in `docs/` in the same PR.
- PR bodies use the template sections: What changed, Tests, Proof, Deferred.

## Commit Messages

Use Conventional Commits: `type(scope): description`
- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`
- Example: `feat(deploy): add blue-green deployment strategy`

## Security Guidelines for Code Changes

When making code changes, consider these security implications:

### Authentication & Authorization
- Never expose sensitive data in error messages
- Always use parameterized queries (prevent SQL injection)
- Validate all user inputs
- Use constant-time comparison for secrets/tokens

### Secrets Management
- Never log secrets, tokens, or passwords
- Don't hardcode credentials - use environment variables
- Clear sensitive data from memory when no longer needed

### Container Security
- Don't run containers as root unless necessary
- Validate image names before pulling
- Sanitize user inputs used in container names/labels
- Limit container capabilities

### API Security
- Validate all CSRF tokens on state-changing operations
- Implement proper rate limiting
- Don't expose internal error details to clients

### Data Protection
- Use proper password hashing (Argon2id - already implemented)
- Encrypt sensitive data at rest where applicable
- Sanitize log output for PII

* Go version must NEVER be dropped below the official latest release (currently 1.26).

## Cursor Cloud specific instructions

The Cloud Agent VM has no systemd. PostgreSQL 16, Docker, and the dev server are started for you.

- App: `http://127.0.0.1:8080` via `make dev` (`GOFORGE_DEV_MODE=true`). `/health` and `/health/ready` report database and Docker. If port 8080 is already healthy, do not start a second server.
- Database: `postgres://goforge:password@localhost:5432/goforge?sslmode=disable` (the dev default). `make test` uses this database when `DATABASE_URL` is unset. Postgres is started with `pg_ctlcluster`, not `service`.
- Docker uses `fuse-overlayfs` and legacy iptables (`/etc/docker/daemon.json`). One-click services join the `traefik` network; create it with `docker network create traefik` if it is missing.
- `internal/web/templates/**/*_templ.go` and `static/css/output.css` are gitignored. Regenerate with `make templ` and `make css`. The Tailwind CLI on `PATH` is v3.4.17; the v4 binary does not read `tailwind.config.js`.
- Use `/usr/local/go/bin/go`, and it must be Go 1.26.x: `go.mod` says `go 1.26.0`. `/usr/bin/go` is Ubuntu's Go 1.22 and cannot read the `tool` directive in `go.mod`. `make test` needs the full 1.26 toolchain; an older Go auto-downloads a toolchain module that is missing `covdata`.
- `air` is `/usr/local/bin/air` (v1.67.0). `templ` and `golangci-lint` run through `go tool`. Local registration works without GitHub OAuth secrets.
