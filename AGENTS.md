# AGENTS.md - AI Coding Agent Guidelines

This document provides guidelines for AI coding agents working in the GoCool codebase.

## Project Overview

GoCool is a lightweight, self-hosted PaaS built in Go with HTMX + templ. It provides
git-to-deploy workflows, one-click services, automatic SSL via Traefik, and real-time
monitoring. Tech stack: Go 1.22+, PostgreSQL, Docker, HTMX 2, templ, TailwindCSS.

## Build, Lint, and Test Commands

```bash
# Build
make build                          # Build production binary (runs templ + css first)
make dev                            # Run with hot reload via air

# Format and Lint
make fmt                            # Format code with gofmt -s -w
make fmt-check                      # Check formatting (CI mode, exits non-zero if unformatted)
make vet                            # Run go vet
make lint                           # Run golangci-lint (30+ linters configured)
make check                          # Run all checks: fmt-check, vet, lint, test

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

# Code generation
make templ                          # Generate templ templates (*_templ.go files)
make css                            # Compile TailwindCSS

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

    // Local packages (github.com/user/gocool/...)
    "github.com/user/gocool/internal/config"
    "github.com/user/gocool/internal/models"
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
cmd/gocool/              # Entry point (main.go)
internal/
  auth/                  # Authentication (GitHub OAuth, sessions, CSRF)
  config/                # Configuration loading from env
  database/              # PostgreSQL connection, migrations, repositories
  deploy/                # Deployment pipeline and orchestration
  docker/                # Docker SDK wrapper (build, container, logs)
  git/                   # Git providers (GitHub, GitLab, Gitea)
  models/                # Domain models (structs)
  monitor/               # Resource monitoring (CPU, memory, disk)
  project/               # Project and environment management
  proxy/                 # Traefik integration
  services/              # One-click service catalog
  sse/                   # Server-Sent Events hub
  web/
    handlers/            # HTTP handlers
    templates/
      layouts/           # Base templates (base.templ)
      components/        # Reusable components (nav.templ, modal.templ)
      pages/             # Page templates
static/                  # CSS, JS (htmx.min.js)
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

## Commit Messages

Use Conventional Commits: `type(scope): description`
- Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`
- Example: `feat(deploy): add blue-green deployment strategy`
