# Development Setup

Set up a local GoForge development environment.

## Prerequisites

- Go 1.25+
- Docker Engine
- PostgreSQL (local or Docker)
- templ (managed via `go tool templ`)
- air (for hot reload)

See [Prerequisites](../getting-started/prerequisites.md) for installation instructions.

## Setup Steps

### 1. Clone the Repository

```bash
git clone https://github.com/raythurman2386/goforge.git
cd goforge
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Generate Templates

```bash
make templ
```

### 4. Install Other Development Tools

```bash
make install-tools
```

This installs:

- `golangci-lint` -- Linter suite
- `air` -- Hot reload

Note: `templ` is managed via `go tool` and doesn't need manual installation if you use `make templ`.

### 4. Start PostgreSQL

```bash
docker run -d \
  --name goforge-db \
  -e POSTGRES_USER=goforge \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=goforge \
  -p 5432:5432 \
  postgres:15-alpine
```

### 5. Configure Environment

```bash
cp .env.example .env
# The defaults work for local development
# Ensure GOFORGE_DEV_MODE=true is set
```

### 6. Run Migrations

```bash
make migrate
```

### 7. Start Development Server

```bash
make dev
```

This runs `air`, which:

- Watches `.go`, `.templ`, and `.sql` files for changes
- Regenerates templ templates automatically
- Rebuilds and restarts the server

## Available Make Targets

```bash
make help          # Show all available targets

# Build
make build         # Build production binary
make dev           # Run with hot reload

# Code Quality
make fmt           # Format code
make fmt-check     # Check formatting (CI mode)
make vet           # Run go vet
make lint          # Run golangci-lint
make check         # Run all checks (fmt, vet, lint, test)

# Testing
make test          # Run all tests with race detector
make test-short    # Run tests without race detector
make test-coverage # Generate HTML coverage report

# Code Generation
make templ         # Generate templ templates
make css           # Compile TailwindCSS

# Database
make migrate       # Run migrations up
make migrate-down  # Rollback last migration
make migrate-create name=xyz  # Create new migration

# Dependencies
make tidy          # Run go mod tidy
make deps          # Download dependencies

# Docker
make docker-build  # Build Docker image
make docker-run    # Run Docker container

# Cleanup
make clean         # Remove build artifacts
```

## Project Layout

Familiarize yourself with the [Project Structure](../architecture/project-structure.md) to understand where code lives.

## IDE Setup

### VS Code

Recommended extensions:

- Go (`golang.go`)
- templ (`a-h.templ`)
- Tailwind CSS IntelliSense (`bradlc.vscode-tailwindcss`)
- EditorConfig (`EditorConfig.EditorConfig`)

### GoLand / IntelliJ

- Install the Go plugin
- File watchers for templ generation (optional -- `make dev` handles this)

## Debugging

## Limitations

`make dev` runs the application locally without Docker integration. This means:

- **Service deployment** (Redis, PostgreSQL, etc.) requires Docker socket access
- **Project deployments** with container building requires Docker

To test features that require Docker:

```bash
# Use the full Docker stack instead
docker-compose up -d
```

### Debug with Delve

```bash
# Install Delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Run with debugger
dlv debug ./cmd/goforge -- serve
```

### Environment Variables

In dev mode, GoForge auto-generates secrets and enables debug logging. If you need to test production behavior locally:

```bash
GOFORGE_DEV_MODE=false \
ENCRYPTION_KEY=$(openssl rand -base64 32) \
SESSION_SECRET=$(openssl rand -base64 32) \
CSRF_SECRET=$(openssl rand -base64 32) \
make dev
```
