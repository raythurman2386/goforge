# Design Patterns

GoForge employs several well-established design patterns. This page documents the key patterns and how they are implemented.

## Repository Pattern

All database access goes through repository structs that encapsulate SQL queries behind clean method signatures.

```go
// Each entity has a dedicated repository
type ProjectRepository struct {
    db *sql.DB
}

func (r *ProjectRepository) Create(ctx context.Context, p *models.Project) error { ... }
func (r *ProjectRepository) GetByID(ctx context.Context, id string) (*models.Project, error) { ... }
func (r *ProjectRepository) List(ctx context.Context, userID string) ([]*models.Project, error) { ... }
```

**Benefits:**

- SQL is isolated from business logic
- Consistent data access patterns across all entities
- Encryption of sensitive fields (tokens, keys) happens transparently in the repository layer

**Current limitation:** Repositories are concrete types rather than interfaces, which makes unit testing harder. Business logic services depend directly on `*repositories.UserRepository` rather than an interface.

## Service Layer

Business logic lives in service structs that sit between HTTP handlers and repositories.

```go
type Service struct {
    projectRepo     *repositories.ProjectRepository
    environmentRepo *repositories.EnvironmentRepository
    deploymentRepo  *repositories.DeploymentRepository
    // ...
}

func (s *Service) CreateProject(ctx context.Context, input CreateProjectInput) (*models.Project, error) {
    // Validation, business rules, then delegate to repository
}
```

**Benefits:**

- Handlers stay thin (parse request, call service, render response)
- Business logic is reusable across different entry points (HTTP, CLI, webhooks)
- Services coordinate between multiple repositories and infrastructure components

## State Machine

Deployment status transitions are governed by an explicit state machine in `internal/deploy/state.go`.

```go
func isValidTransition(from, to models.DeploymentStatus) bool {
    // Any state can transition to failed or canceled
    if to == models.DeploymentStatusFailed || to == models.DeploymentStatusCanceled {
        return true
    }

    switch from {
    case models.DeploymentStatusPending:
        return to == models.DeploymentStatusCloning
    case models.DeploymentStatusCloning:
        return to == models.DeploymentStatusBuilding
    case models.DeploymentStatusBuilding:
        return to == models.DeploymentStatusDeploying
    case models.DeploymentStatusDeploying:
        return to == models.DeploymentStatusRunning
    case models.DeploymentStatusRunning:
        return to == models.DeploymentStatusRolledBack
    default:
        return false
    }
}
```

Each transition is validated before being persisted to the database.

## Worker Queue

Deployments are processed asynchronously through a PostgreSQL-backed persistent queue. The queue ensures deployments survive server restarts and enables automatic recovery of orphaned jobs.

```go
type Worker struct {
    queueRepo    *repositories.QueueRepository  // Database-backed queue
    pollInterval time.Duration                 // Polling interval when queue is empty
    orphanTimeout time.Duration                // Timeout before requeuing stale jobs
    quit        chan struct{}
    service     *Service
}
```

The HTTP handler enqueues a deployment ID via `Worker.Enqueue()`, and the worker goroutine polls the database for pending jobs, runs the pipeline, and updates the status. This prevents long-running builds from blocking HTTP requests and ensures no deployments are lost on restart.

## Pub/Sub (Server-Sent Events)

The SSE Hub implements a publish-subscribe pattern for real-time updates.

```go
type Hub struct {
    clients    map[chan Event]string // client channel -> topic
    register   chan clientInfo
    unregister chan chan Event
    broadcast  chan Event
    quit       chan struct{}
    mu         sync.RWMutex
}

type clientInfo struct {
    ch    chan Event
    topic string
}
```

Clients subscribe to topics (e.g., `deployment:abc123`, `container:stats`), and messages are broadcast only to subscribers of matching topics. This powers live deployment logs, container stats, and build output in the UI.

## HMAC CSRF Tokens

CSRF protection uses HMAC tokens keyed by `CSRF_SECRET`, adapted for HTMX:

1. Signed in, the token is derived from the session (`HMAC(secret, SHA-256(session token))`); signed out, it is a random nonce plus its HMAC, stored in the `goforge_csrf` cookie
2. Templates render the token into `hx-headers` (`X-CSRF-Token`) and forms (`csrf_token`)
3. Server middleware recomputes the expected token and compares in constant time; an unsigned cookie value never verifies

## Middleware Chain

The HTTP middleware chain follows a layered security model:

```
Request
  -> Recovery (panic handler)
    -> Security headers
      -> Rate Limiting (client IP)
        -> Session Loading (optional auth)
          -> CSRF Validation (state-changing methods)
            -> Handler
```

## Template Composition

Templates use templ's component model for composition:

```
base.templ (HTML shell, sidebar, scripts)
  └── page.templ (page-specific content)
       └── component.templ (reusable UI elements)
```

HTMX attributes on components enable partial page updates without full reloads:

```html
<button hx-post="/projects/deploy" hx-target="#deployment-status" hx-swap="innerHTML">
  Deploy
</button>
```

## Provider Pattern (Git)

Git operations are abstracted behind a provider interface to support multiple hosting platforms:

```go
type Provider interface {
    ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error)
    ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error)
    ListTags(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error)
    GetFileContent(ctx context.Context, source *models.GitSource, owner, repo, ref, path string) (string, error)
}
```

Implementations exist for GitHub (using `go-github`), GitLab (REST API), and Gitea (REST API). The `Service` routes calls to the appropriate provider based on the `GitSource.Type` field.

## Encryption at Rest

Sensitive data is encrypted before database storage using AES-256-GCM:

```go
type EncryptionService struct {
    key []byte
}

func (e *EncryptionService) Encrypt(plaintext string) (string, error) { ... }
func (e *EncryptionService) Decrypt(ciphertext string) (string, error) { ... }
```

Encryption is applied transparently in the repository layer for fields like:

- GitHub access tokens (`UserRepository`)
- Git source access tokens and SSH keys (`GitSourceRepository`)
- Environment variable secrets (`EnvVariableRepository`)
