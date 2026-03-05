# Code Review Findings

This document consolidates findings from a comprehensive code review of the GoForge codebase. Findings are organized by severity and category.

---

## Summary

| Severity | Count |
|----------|-------|
| Critical (Security) | 3 |
| High (Security) | 5 |
| Critical (Bug) | 5 |
| High (Bug) | 3 |
| Medium (Quality) | 10 |
| Low (Quality/Config) | 8 |

---

## Critical Security Issues

### ~~1. CSRF Token Timing Attack~~ (Resolved)

**File**: `internal/auth/csrf.go:54`

The CSRF token is compared using the `!=` operator, which is vulnerable to timing attacks:

```go
if requestToken == "" || requestToken != token {
```

**Fix**: Use `crypto/subtle.ConstantTimeCompare()`:

```go
import "crypto/subtle"

if requestToken == "" || subtle.ConstantTimeCompare([]byte(requestToken), []byte(token)) != 1 {
```

---

### 2. Default Database Credentials in Production

**File**: `internal/config/config.go:144`

The `applyDefaults()` function runs before `validate()`, which means default credentials (including `password` as the database password) are applied even in production if the environment variable is not set. The application starts successfully with insecure defaults rather than failing fast.

**Fix**: Move validation before defaults, or require `DATABASE_URL` in production with no fallback default.

---

### 3. Insecure SSH Host Key Verification

**File**: `internal/git/ssh.go`

Uses `ssh.InsecureIgnoreHostKey()` for SSH connections, disabling host key verification entirely. This enables man-in-the-middle attacks on git clone operations.

**Fix**: Implement known_hosts-based verification or allow users to supply expected host keys per git source.

---

### ~~4. OAuth State Validation~~ (Resolved)

**File**: `internal/web/handlers/auth.go:157-179`

OAuth state validation IS implemented. The GitHub OAuth flow generates a UUID state value, stores it in a `github_oauth_state` cookie (HttpOnly, 5-minute expiry), and verifies it on callback. No action needed.

---

## High Security Issues

### 5. CSRF Cookie Missing `Secure` Flag

**File**: `internal/auth/csrf.go:35-41`

The CSRF cookie is set without `Secure: true`, allowing it to be transmitted over plain HTTP:

```go
http.SetCookie(w, &http.Cookie{
    Name:     CSRFCookieName,
    Value:    token,
    Path:     "/",
    HttpOnly: false,
    SameSite: http.SameSiteLaxMode,
    // Missing: Secure: true
})
```

---

### 6. CSRF Token Not Bound to Session

**File**: `internal/auth/csrf.go`

The CSRF token is a standalone random value with no cryptographic binding to the user's session. The `CSRFSecret` configuration field exists but is unused.

**Fix**: Generate CSRF tokens as an HMAC of the session ID using `CSRFSecret` as the key, or use the double-submit pattern with session binding.

---

### 7. No Session Rotation After Login

**File**: `internal/auth/auth.go`

After successful email/password or OAuth authentication, the session is created but any pre-existing session token is not invalidated. This creates a session fixation vulnerability.

**Fix**: Destroy any existing session and issue a new token upon successful authentication.

---

### 8. Email Enumeration via Timing

**File**: `internal/auth/auth.go` (Authenticate method)

The authentication flow returns early if the user is not found (skipping password comparison), creating a measurable timing difference that reveals whether an email address is registered.

**Fix**: Always perform an Argon2id comparison even when the user is not found (compare against a dummy hash).

---

### 9. WriteTimeout Kills SSE Connections

**File**: `cmd/goforge/main.go`

The HTTP server's `WriteTimeout: 15s` applies to all connections, including SSE endpoints (`/containers/{id}/stats`, `/containers/{id}/logs`, `/projects/{id}/deployments/{deploymentID}/logs`). These long-lived connections will be terminated after 15 seconds.

**Fix**: Use `http.TimeoutHandler` selectively on non-SSE routes, or set `WriteTimeout: 0` and implement per-handler timeouts.

---

## Critical Bugs

### ~~10. Docker Build Errors Not Detected~~ (Resolved)

**File**: `internal/docker/build.go`

When building Docker images, the build output stream is read but error messages embedded in the JSON stream (Docker's `{"errorDetail": {...}, "error": "..."}` format) are not checked. A failed build (e.g., syntax error in Dockerfile) will appear successful.

**Fix**: Parse each JSON message from the build output and check for the `error` field:

```go
type buildMessage struct {
    Stream      string `json:"stream"`
    Error       string `json:"error"`
    ErrorDetail struct {
        Message string `json:"message"`
    } `json:"errorDetail"`
}
```

---

### 11. Deployment Queue Blocks Indefinitely

**File**: `internal/deploy/queue.go` (Enqueue method)

The `Enqueue(id string)` method sends to a buffered channel without a `select` statement. If the queue is full, the calling goroutine (HTTP handler) blocks indefinitely until a slot opens.

**Fix**: Use a `select` with context cancellation or a timeout:

```go
func (w *Worker) Enqueue(ctx context.Context, id string) error {
    select {
    case w.queue <- id:
        return nil
    case <-ctx.Done():
        return fmt.Errorf("queue full, timed out: %w", ctx.Err())
    }
}
```

---

### 12. Container Leak on Start Failure

**File**: `internal/docker/container.go`

If `ContainerCreate` succeeds but `ContainerStart` fails, the created container is not cleaned up, leaving orphaned containers on the Docker host.

**Fix**: Add cleanup in the error path:

```go
if err := cli.ContainerStart(ctx, resp.ID, ...); err != nil {
    _ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
    return "", fmt.Errorf("failed to start container: %w", err)
}
```

---

### 13. Health Check Timeout Misapplied

**File**: `internal/deploy/health.go` (CheckWithRetry)

The function creates a single context with `healthCheckTimeout` and uses it for ALL retry attempts. If the timeout is (say) 30 seconds and there are 10 retries with 3-second intervals, the timeout expires well before all retries can execute. The timeout should be the total allowed duration for all attempts combined, not a per-attempt value.

---

### 14. PruneImages Removes All Dangling Images

**File**: `internal/deploy/cleanup.go`

`PruneImages()` calls the Docker API's image prune without filtering by GoForge labels. This removes ALL unused/dangling images on the host, including those belonging to other applications.

**Fix**: Add a label filter:

```go
filters := filters.NewArgs()
filters.Add("label", "managed-by=goforge")
```

---

## High Bugs

### 15. Blue-Green Deployer Incomplete

**File**: `internal/deploy/bluegreen.go`

The blue-green deployment strategy creates a new container alongside the old one, but does not actually switch Traefik routing labels from the old container to the new one. Traffic continues flowing to the old container.

---

### 16. Git Provider Pagination Missing

**Files**: `internal/git/github.go`, `internal/git/gitlab.go`, `internal/git/gitea.go`

All three git provider implementations fetch only the first page of results (default page size, typically 30-100 items). Users with more repositories or branches than the page size will see truncated lists.

**Fix**: Implement pagination by following `Link` headers or incrementing page numbers until an empty response is received.

---

### 17. Single Deployment Worker Bottleneck

**File**: `internal/deploy/queue.go`

The deployment queue uses a single goroutine worker to process deployments sequentially. During a build (which can take minutes), all other deployments are queued and waiting.

**Fix**: Implement a worker pool with configurable concurrency.

---

## Medium Quality Issues

### 18. 13-Parameter Constructor

**File**: `internal/deploy/service.go`

The `NewService()` constructor accepts 13 parameters, making it difficult to use and maintain. This is a strong signal that the struct has too many responsibilities.

**Fix**: Use an options struct or builder pattern:

```go
type DeploymentServiceConfig struct {
    Repo     DeploymentRepository
    Docker   DockerClient
    Git      GitService
    // ...
}
```

---

### 19. fmt.Printf Instead of Structured Logger

**Files**: `internal/deploy/state.go`, `internal/deploy/cleanup.go`, `internal/deploy/rollback.go`

Several deployment package files use `fmt.Printf` for logging instead of the structured `slog` logger used elsewhere in the codebase. This creates inconsistent log output and loses structured fields.

---

### ~~20. Secret Values Not Encrypted~~ (Resolved)

**File**: `internal/database/repositories/env_variable.go`

Environment variable secrets ARE encrypted at rest using AES-256-GCM via `secrets.EncryptionService`. Git source tokens and user access tokens are also encrypted. No action needed.

---

### 21. ~~Stale Project Name in Linter Config~~ (Resolved)

**File**: `.golangci.yml`

~~Contains references to the old project name "gocool".~~ **Fixed** — `.golangci.yml` now uses `github.com/raythurman2386/goforge` as the local prefix.

---

### 22. CGO_ENABLED=0 Conflicts with go-sqlite3

**Files**: `Makefile`, `Dockerfile`

The build uses `CGO_ENABLED=0` (static linking), but `go-sqlite3` requires CGO. If SQLite support is used (even transitively), the build will fail silently or at runtime.

---

### 23. Systemd Service File Missing Subcommand

**File**: `deploy/goforge.service`

The `ExecStart` line doesn't pass a subcommand to the binary, and the binary path is `/opt/goforge/goforge` (not `/usr/local/bin/goforge`):

```ini
ExecStart=/opt/goforge/goforge
```

Additionally, the service file runs as `User=root` by default. See the [Systemd Deployment](deployment/systemd.md) page for the recommended non-root setup.

---

### 24. CI Installs templ 5 Times (Unpinned)

**File**: `.github/workflows/ci.yml`

The CI workflow installs `templ@latest` multiple times across different job steps, and the version is not pinned to match the `go.mod` version (v0.3.1001). This can cause build inconsistencies.

**Fix**: Install once, pin to the version in `go.mod`, and cache the binary.

---

### 25. Docker Compose Exposes PostgreSQL

**File**: `docker-compose.yml`

The production Docker Compose file maps PostgreSQL port `5432:5432` to the host, making it accessible from outside the Docker network.

**Fix**: Remove the host port mapping or bind to localhost only (`127.0.0.1:5432:5432`).

---

### 26. Dockerfile Runs as Root

**File**: `Dockerfile`

The GoForge container runs as root. If the application is compromised, the attacker has root access within the container.

**Fix**: Add a non-root user:

```dockerfile
RUN adduser -D -g '' goforge
USER goforge
```

---

### 27. Bug Report Template References Old Name

**File**: `.github/ISSUE_TEMPLATE/bug_report.yml`

Contains references to "GoCool" instead of "GoForge".

---

### 28. Static File Server Under CSRF Middleware

**File**: `internal/web/router.go:58`

The static file server is placed within the CSRF middleware group. While GET requests are not validated, this is unnecessary overhead. Static files should be served outside the CSRF middleware group.

---

### 29. No Input Validation on Project Names

**File**: `internal/web/handlers/project_handler.go`

Project names are used in Docker image tags and container names but are not validated for characters that could cause issues (spaces, special characters, etc.).

---

## Low Priority / Cleanup

### 30. Missing `defer rows.Close()` Patterns

Several repository methods should verify that `sql.Rows` are properly closed in all code paths. The `sqlclosecheck` linter should catch these, but manual review is recommended.

### 31. Context Cancellation Not Checked in Loops

Long-running operations (deployment pipeline stages) should periodically check `ctx.Err()` to respect cancellation signals.

### 32. No Graceful Shutdown for SSE Connections

When the server shuts down, active SSE connections are terminated without notification. Clients should receive a close event.

### 33. Hardcoded Docker Network Name

The Docker network name "goforge" is hardcoded in several places. This should be configurable.

### 34. No Database Connection Pooling Configuration

The database connection pool uses default settings. For production, `MaxOpenConns`, `MaxIdleConns`, and `ConnMaxLifetime` should be configurable.

### 35. Missing Request ID Middleware

There is no request ID middleware for correlating logs across a single request lifecycle.

### 36. No Structured Error Responses for API Routes

API routes (under `/api/git/*`) return HTML error responses rather than JSON, which can be problematic for programmatic consumers.

### 37. Template Caching

Templ templates are compiled at build time (good), but there's no ETag or caching headers on static assets. Adding cache-busting and proper cache headers would improve performance.

---

## Recommendations Summary

### Immediate (before production)

1. Fix CSRF timing attack (use `ConstantTimeCompare`)
2. ~~Add OAuth state validation~~ (already implemented)
3. Detect Docker build errors in output stream
4. Fix deployment queue blocking
5. Remove default database credentials in production
6. Clean up containers on start failure

### Short-term

7. Add `Secure` flag to CSRF cookie
8. Implement session rotation after login
9. Fix PruneImages to filter by GoForge labels
10. Pin templ version in CI
11. Remove PostgreSQL host port mapping
12. Add non-root user to Dockerfile
13. Fix systemd service file

### Medium-term

14. ~~Implement secret value encryption~~ (already implemented)
15. Add SSH host key verification
16. Complete blue-green deployment Traefik label switching
17. Add git provider pagination
18. Refactor 13-parameter constructor
19. Replace `fmt.Printf` with structured logger
20. Add deployment worker pool
