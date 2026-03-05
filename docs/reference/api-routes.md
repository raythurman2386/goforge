# API Routes Reference

This is a complete reference of all HTTP routes exposed by the GoForge server.

## Middleware Stack

All routes pass through the following middleware (in order):

1. **Recoverer** - Catches panics and returns 500
2. **RateLimit** - 30 requests/second, burst of 60
3. **Session** - Populates user/session in request context (safe for all routes)

Routes under the UI group additionally use:

4. **CSRFMiddleware** - CSRF token validation for state-changing methods (POST, PUT, DELETE, PATCH)

Authenticated routes additionally use:

5. **RequireAuth** - Redirects to `/login` (or returns `HX-Redirect` header for HTMX requests)

---

## Webhooks

These routes are **not** protected by CSRF (machine-to-machine communication).

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| `POST` | `/webhooks/github` | `webhookHandler.HandleGitHub` | No | GitHub push webhook receiver |
| `POST` | `/webhooks/gitlab` | `webhookHandler.HandleGitLab` | No | GitLab push webhook receiver |
| `POST` | `/webhooks/gitea` | `webhookHandler.HandleGitea` | No | Gitea push webhook receiver |

---

## Static Files & Health Checks

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| `GET` | `/static/*` | File server | No | Serves static assets (CSS, JS) |
| `GET` | `/health` | `healthHandler.Health` | No | Basic health check (liveness) |
| `GET` | `/health/ready` | `healthHandler.HealthReady` | No | Readiness check (DB connectivity) |

---

## Public Routes (Authentication)

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| `GET` | `/` | `handlers.Index` | No | Landing/home page |
| `GET` | `/login` | `authHandler.LoginPage` | No | Login form |
| `POST` | `/auth/login` | `authHandler.Login` | No | Process login (email/password) |
| `GET` | `/auth/register` | `authHandler.RegisterPage` | No | Registration form |
| `POST` | `/auth/register` | `authHandler.Register` | No | Process registration |
| `GET` | `/auth/github` | `authHandler.GitHubLogin` | No | Initiate GitHub OAuth flow |
| `GET` | `/auth/github/callback` | `authHandler.GitHubCallback` | No | GitHub OAuth callback |
| `POST` | `/auth/logout` | `authHandler.Logout` | No | Destroy session and logout |

---

## Authenticated Routes

All routes below require an active session (enforced by `RequireAuth` middleware).

### Dashboard

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/dashboard` | `dashboardHandler.Dashboard` | Main dashboard view |

### Git API

Used by HTMX components for dynamic repository/branch selection.

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/api/git/repos` | `gitHandler.ListRepositories` | List repos (query-param `?source_id=`) |
| `GET` | `/api/git/branches` | `gitHandler.ListBranches` | List branches (query-param based) |
| `GET` | `/api/git/sources/{sourceId}/repos` | `gitHandler.ListRepositories` | List repos for a git source |
| `GET` | `/api/git/sources/{sourceId}/repos/{owner}/{repo}/branches` | `gitHandler.ListBranches` | List branches for a repo |
| `GET` | `/api/git/sources/{sourceId}/repos/{owner}/{repo}/tags` | `gitHandler.ListTags` | List tags for a repo |

### Projects

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/projects` | `projectHandler.ListProjects` | List all projects |
| `GET` | `/projects/new` | `projectHandler.NewProjectPage` | New project form |
| `POST` | `/projects` | `projectHandler.CreateProject` | Create a project |
| `GET` | `/projects/{id}` | `projectHandler.GetProject` | View project details |
| `PUT` | `/projects/{id}` | `projectHandler.UpdateProject` | Update project |
| `PUT` | `/projects/{id}/settings` | `projectHandler.UpdateSettings` | Update project settings |
| `DELETE` | `/projects/{id}` | `projectHandler.DeleteProject` | Delete project and resources |
| `GET` | `/projects/{id}/deployments` | `projectHandler.GetProject` | Project deployments tab |
| `GET` | `/projects/{id}/settings` | `projectHandler.GetProject` | Project settings tab |

### Deployments

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `POST` | `/projects/{id}/deployments` | `deploymentHandler.TriggerDeployment` | Trigger a new deployment |
| `GET` | `/projects/{id}/deployments/{deploymentID}` | `deploymentHandler.GetDeployment` | View deployment details |
| `GET` | `/projects/{id}/deployments/{deploymentID}/status` | `deploymentHandler.GetDeploymentStatus` | Poll deployment status (HTMX) |
| `POST` | `/projects/{id}/deployments/{deploymentID}/rollback` | `deploymentHandler.RollbackDeployment` | Roll back to this deployment |
| `POST` | `/projects/{id}/deployments/{deploymentID}/cancel` | `deploymentHandler.CancelDeployment` | Cancel a running deployment |
| `GET` | `/projects/{id}/deployments/{deploymentID}/logs` | `sseHandler.DeploymentLogs` | Stream deployment logs (SSE) |

### Environments

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/projects/{id}/envs` | `envHandler.ListEnvironments` | List project environments |
| `POST` | `/projects/{id}/envs` | `envHandler.CreateEnvironment` | Create an environment |
| `PUT` | `/projects/{id}/envs/{envId}` | `envHandler.UpdateEnvironment` | Update an environment |
| `GET` | `/projects/{id}/envs/new` | `envHandler.NewEnvironmentModal` | New environment modal (HTMX) |

### Environment Variables

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/envs/{envId}/variables` | `envHandler.GetVariables` | List environment variables |
| `POST` | `/envs/{envId}/variables` | `envHandler.AddVariable` | Add a variable |
| `GET` | `/envs/{envId}/variables/new` | `envHandler.NewVariableModal` | New variable modal (HTMX) |

### Service Catalog

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/services` | `serviceHandler.Catalog` | Browse service catalog |
| `POST` | `/services` | `serviceHandler.Deploy` | Deploy a service (fallback) |
| `GET` | `/services/{template}/configure` | `serviceHandler.Configure` | Service configuration form |
| `POST` | `/services/{template}` | `serviceHandler.Deploy` | Deploy a service by template |
| `GET` | `/services/{id}` | `serviceHandler.Show` | View service instance (`{id}` is UUID) |
| `DELETE` | `/services/{id}` | `serviceHandler.Delete` | Delete a service instance |
| `POST` | `/services/{id}/restart` | `serviceHandler.Restart` | Restart a service instance |

!!! note "Route Disambiguation"
    The `{id}` parameter for service instances uses a UUID regex pattern (`[a-f0-9-]{36}`) to disambiguate from `{template}` string routes.

### Containers (SSE Streams)

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/containers/{id}/stats` | `sseHandler.ContainerStats` | Stream container metrics (SSE) |
| `GET` | `/containers/{id}/logs` | `sseHandler.ContainerLogs` | Stream container logs (SSE) |

### Settings

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `POST` | `/settings/prune` | `settingsHandler.PruneSystem` | Prune Docker system resources |
| `GET` | `/settings/git` | `settingsHandler.GitSources` | List configured git sources |
| `POST` | `/settings/git` | `settingsHandler.CreateGitSource` | Add a git source |
| `GET` | `/settings/git/new` | `settingsHandler.NewGitSourceModal` | New git source modal (HTMX) |
| `DELETE` | `/settings/git/{id}` | `settingsHandler.DeleteGitSource` | Remove a git source |

---

## Error Handlers

| Status | Handler | Description |
|--------|---------|-------------|
| 404 | `handlers.NotFoundHandler` | Custom not found page |
| 405 | `handlers.MethodNotAllowedHandler` | Method not allowed response |

---

## SSE (Server-Sent Events)

The following endpoints use Server-Sent Events for real-time streaming:

- **`/projects/{id}/deployments/{deploymentID}/logs`** - Streams deployment build and runtime logs in real-time
- **`/containers/{id}/stats`** - Streams CPU, memory, network I/O metrics
- **`/containers/{id}/logs`** - Streams container stdout/stderr output

SSE connections are long-lived HTTP connections. Clients should handle reconnection on disconnect.

!!! warning "Server Timeout"
    The default `WriteTimeout` of 15 seconds in the HTTP server configuration may prematurely terminate SSE connections. Consider using `http.TimeoutHandler` selectively on non-SSE routes, or setting `WriteTimeout: 0` and implementing per-handler timeouts.

---

## CSRF Protection

All state-changing methods (`POST`, `PUT`, `DELETE`, `PATCH`) under the UI routes group require a valid CSRF token. The token can be provided via:

- **Header**: `X-CSRF-Token` (used by HTMX via `hx-headers`)
- **Form field**: `csrf_token` (used by standard HTML forms)

The CSRF token is stored in a cookie named `goforge_csrf` and is accessible to JavaScript (not HttpOnly) for HTMX integration.

Webhook routes (`/webhooks/*`) are explicitly excluded from CSRF protection.

---

## Source

Route definitions: `internal/web/router.go`
