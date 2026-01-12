# Contributing to GoCool

Thank you for your interest in contributing to GoCool! This document provides guidelines and information for contributors.

## Code of Conduct

Be respectful, inclusive, and constructive. We're all here to build something useful.

## Getting Started

### Prerequisites

- Go 1.22+
- Docker Engine 24+
- PostgreSQL 15+
- templ CLI
- golangci-lint

### Setup

1. Fork and clone the repository
2. Install dependencies:
   ```bash
   go mod download
   go install github.com/a-h/templ/cmd/templ@latest
   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
   ```
3. Copy environment file:
   ```bash
   cp .env.example .env
   ```
4. Start PostgreSQL and run migrations:
   ```bash
   make migrate
   ```
5. Run the development server:
   ```bash
   make dev
   ```

## Development Workflow

### Branch Naming

- `feature/description` — New features
- `fix/description` — Bug fixes
- `refactor/description` — Code refactoring
- `docs/description` — Documentation updates

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): description

[optional body]
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

Examples:
```
feat(deploy): add blue-green deployment strategy
fix(auth): handle expired GitHub tokens gracefully
docs(readme): add Docker Compose instructions
```

### Before Submitting

Run all checks:

```bash
make fmt       # Format code
make lint      # Run linters
make test      # Run tests
```

All checks must pass. The CI will verify this automatically.

## Code Style

### Go

- Follow [Effective Go](https://golang.org/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `gofmt` (enforced by CI)
- Keep functions focused and small
- Write table-driven tests where appropriate
- Document exported functions and types

### Templates (templ)

- Components go in `internal/web/templates/components/`
- Pages go in `internal/web/templates/pages/`
- Keep templates simple; logic belongs in handlers
- Use semantic HTML

### CSS (Tailwind)

- Use Tailwind utility classes
- Extract components for repeated patterns
- Keep the design system consistent

## Project Structure

```
cmd/gocool/          # Application entry point
internal/
  auth/              # Authentication (GitHub OAuth, sessions)
  config/            # Configuration loading
  database/          # Database connection, migrations, repositories
  deploy/            # Deployment pipeline
  docker/            # Docker SDK wrapper
  git/               # Git providers (GitHub, GitLab, Gitea)
  models/            # Domain models
  monitor/           # Resource monitoring
  project/           # Project management
  proxy/             # Traefik integration
  services/          # One-click service catalog
  sse/               # Server-Sent Events
  web/               # HTTP handlers and templates
static/              # Static assets (CSS, JS)
templates/           # Service template YAML files
```

## Testing

### Running Tests

```bash
make test                    # All tests
go test ./internal/auth/...  # Specific package
go test -v -run TestName     # Specific test
```

### Writing Tests

- Place tests in `*_test.go` files alongside the code
- Use table-driven tests for multiple cases
- Mock external dependencies (Docker, database, APIs)
- Aim for high coverage on critical paths (auth, deploy, docker)

Example:
```go
func TestDeploymentStateMachine(t *testing.T) {
    tests := []struct {
        name     string
        current  Status
        event    Event
        expected Status
    }{
        {"pending to building", Pending, StartBuild, Building},
        {"building to deploying", Building, BuildSuccess, Deploying},
        // ...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := transition(tt.current, tt.event)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

## Pull Request Process

1. Create a feature branch from `main`
2. Make your changes with clear commits
3. Update documentation if needed
4. Run `make fmt lint test`
5. Push and open a pull request
6. Fill out the PR template
7. Wait for review

### PR Requirements

- All CI checks pass
- At least one approving review
- No merge conflicts
- Commits are clean and well-described

## Reporting Issues

### Bug Reports

Include:
- Go version (`go version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs

### Feature Requests

Include:
- Clear description of the feature
- Use case / problem it solves
- Proposed implementation (if any)

## Questions?

- Open a [Discussion](https://github.com/user/gocool/discussions) for questions
- Check existing issues before creating new ones

Thank you for contributing!
