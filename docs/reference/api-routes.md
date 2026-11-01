# API Routes Reference

This is a complete reference of all HTTP routes exposed by the GoForge server.

## Middleware Stack

All routes pass through the following middleware (in order):

1. **Recoverer** - Catches panics and returns 500
2. **SecurityHeaders** - CSP `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, and HSTS on HTTPS (see [Security](../security.md#security-headers))
3. **RateLimit** - 30 requests/second, burst of 60, per client IP (rightmost untrusted `X-Forwarded-For` hop from a `TRUSTED_PROXIES` peer, or `CF-Connecting-IP` first when `CLOUDFLARE_PROXY_MODE=true`)

The JSON API under `/api/v1` and the MCP endpoint `/mcp` stop here (plus the
15 s timeout). Both authenticate bearer tokens themselves, ignore session
cookies, check token scopes, and are outside the CSRF group. `/mcp` also
refuses cross-origin browser requests (`http.CrossOriginProtection`). See
[API v1](api-v1.md) and [MCP server](../guides/mcp.md).

Every other route (webhooks, UI, and the 404/405 handlers) also uses:

4. **Session** - Populates user/session in request context
5. **APIToken** - If there is no session user and the request carries
   `Authorization: Bearer <token>`, authenticates a legacy `full` token and
   sets the user. An invalid token, or a scoped token (`read`, `write`,
   `protected`), gets `401`: scoped tokens only work on `/api/v1`.

Routes under the UI group additionally use:

6. **CSRF** - HMAC (`CSRF_SECRET`) token validation for state-changing methods (POST, PUT, DELETE, PATCH)

Authenticated routes additionally use:

7. **RequireAuth** - Redirects to `/login` (or returns `HX-Redirect` header for HTMX requests)

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
| `GET` | `/dashboard/stats` | `dashboardHandler.DashboardStats` | Dashboard stats partial (HTMX polling) |
| `POST` | `/dashboard/prune` | `dashboardHandler.Prune` | Prune unused Docker resources from the dashboard |

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
| `GET` | `/projects/{id}/services` | `projectHandler.GetProject` | Project linked-services tab |
| `POST` | `/projects/{id}/services` | `projectHandler.LinkService` | Link a service instance and inject its connection variables |
| `DELETE` | `/projects/{id}/services/{serviceId}` | `projectHandler.UnlinkService` | Unlink a service instance |

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
| `POST` | `/projects/{id}/envs/{envId}/variables/sync` | `envHandler.SyncEnvFromRepo` | Add variables declared in the repo's `.env.example` |

### Environment Variables

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/envs/{envId}/variables` | `envHandler.GetVariables` | List environment variables |
| `POST` | `/envs/{envId}/variables` | `envHandler.AddVariable` | Add a variable |
| `GET` | `/envs/{envId}/variables/new` | `envHandler.NewVariableModal` | New variable modal (HTMX) |
| `DELETE` | `/envs/{envId}/variables/{varId}` | `envHandler.DeleteVariable` | Delete a variable |

### Service Catalog

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/services` | `serviceHandler.Catalog` | Browse service catalog |
| `GET` | `/services/instances` | `serviceHandler.InstanceList` | Your service instances |
| `POST` | `/services` | `serviceHandler.Deploy` | Deploy a service (fallback) |
| `GET` | `/services/{template}/configure` | `serviceHandler.Configure` | Service configuration form |
| `POST` | `/services/{template}` | `serviceHandler.Deploy` | Deploy a service by template |
| `GET` | `/services/{id}` | `serviceHandler.Show` | View service instance (`{id}` is UUID) |
| `DELETE` | `/services/{id}` | `serviceHandler.Delete` | Delete a service instance |
| `POST` | `/services/{id}/restart` | `serviceHandler.Restart` | Restart a service instance |
| `GET` | `/services/{id}/secret` | `serviceHandler.RevealSecret` | Reveal one secret config or connection value (UI only; there is no API v1 or MCP equivalent) |
| `GET` | `/services/{id}/edit` | `serviceHandler.EditConfig` | Edit config form |
| `POST` | `/services/{id}/edit` | `serviceHandler.UpdateConfig` | Save config and recreate the container (named volumes kept) |
| `POST` | `/services/{id}/update` | `serviceHandler.UpdateImage` | Pull the image again and recreate the container (same config and volumes) |

!!! note "Route Disambiguation"
    The `{id}` parameter for service instances uses a UUID regex pattern (`[a-f0-9-]{36}`) to disambiguate from `{template}` string routes.

### Containers

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/containers` | `containerHandler.List` | List all containers |
| `GET` | `/containers/api` | `containerHandler.ListAPI` | List containers (JSON API) |
| `GET` | `/containers/{id}` | `containerHandler.Detail` | Container detail page |
| `POST` | `/containers/{id}/stop` | `containerHandler.Stop` | Stop a container |
| `POST` | `/containers/{id}/restart` | `containerHandler.Restart` | Restart a container |
| `GET` | `/containers/{id}/stats` | `sseHandler.ContainerStats` | Stream container metrics (SSE) |
| `GET` | `/containers/{id}/logs` | `sseHandler.ContainerLogs` | Stream container logs (SSE) |

### Settings

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/settings/profile` | `settingsHandler.Profile` | User profile page |
| `PUT` | `/settings/profile` | `settingsHandler.UpdateProfile` | Update email |
| `PUT` | `/settings/password` | `settingsHandler.UpdatePassword` | Change password |
| `GET` | `/settings/tokens` | `settingsHandler.ListAPITokens` | List API tokens (HTMX partial) |
| `GET` | `/settings/tokens/new` | `settingsHandler.NewAPITokenModal` | New token modal (HTMX) |
| `GET` | `/settings/tokens/new?preset=mcp` | `settingsHandler.NewAPITokenModal` | MCP token modal: `read` + `write` checked, 90 days, no never-expiring option |
| `POST` | `/settings/tokens` | `settingsHandler.CreateAPIToken` | Create API token (optional `scopes`: `read`, `write`, `protected`; none means `full`; `expires_in` 30, 90, 365, or 0 for never). With `preset=mcp`: none means `read` + `write`, `full` and `protected`-only are refused, `expires_in` 30/90/365 (default 90). New tokens start with `gf_`. Audited as `api_token.create` |
| `POST` | `/settings/tokens/{id}/rotate` | `settingsHandler.RotateAPIToken` | New secret on the same row (id, name, scopes, expiry kept); the old secret stops working. Shows the new token once. `404` for another user's token, `400` if expired. Audited as `api_token.rotate` |
| `DELETE` | `/settings/tokens/{id}` | `settingsHandler.DeleteAPIToken` | Revoke API token (`404` for another user's token). Audited as `api_token.revoke` |
| `POST` | `/settings/prune` | `settingsHandler.PruneSystem` | Prune Docker system resources |
| `GET` | `/settings/git` | `settingsHandler.GitSources` | List configured git sources |
| `POST` | `/settings/git` | `settingsHandler.CreateGitSource` | Add a git source |
| `GET` | `/settings/git/new` | `settingsHandler.NewGitSourceModal` | New git source modal (HTMX) |
| `DELETE` | `/settings/git/{id}` | `settingsHandler.DeleteGitSource` | Remove a git source |
| `PUT` | `/settings/backups` | `settingsHandler.UpdateBackupSettings` | Save backup settings, admin only (`403` otherwise). Applies without a restart. A blank secret keeps the stored one. Audited as `backup.settings.update` with changed field names only. Returns the Backups card; `400` names the bad field. `Cache-Control: no-store` |
| `GET` | `/settings/backups/restore-points` | `settingsHandler.RestorePoints` | Read-only Restore points list for the Backups card (HTMX fragment), admin only (`403` otherwise). Lists complete runs in the bucket and the `goforge backup restore` command; no restore action. `Cache-Control: no-store` |
| `PUT` | `/settings/notifications` | `settingsHandler.UpdateNotifySettings` | Save the Notifications form (`enabled`, `kind` = `ntfy`, `webhook`, or `email`, `url`, `token`, `clear_token`; for email `smtp_host`, `smtp_port`, `smtp_tls` = `starttls`, `implicit`, or `none`, `smtp_username`, `smtp_password`, `clear_smtp_password`, `smtp_from`, `smtp_to`), admin only (`403` otherwise). A blank URL, token, or SMTP password keeps the stored one; URL and token apply only to ntfy and webhook, the SMTP fields only to email. Applies without a restart. Audited as `notify.settings.update` with changed field names only. Returns the Notifications card; `400` names the bad field. `Cache-Control: no-store` |
| `POST` | `/settings/notifications/test` | `settingsHandler.TestNotifySettings` | Send one test notification to the form's URL (blank fields use the stored ones), admin only. 10 s limit, saves nothing. `200` with `Test notification sent to <host>` (email: `Test email sent via <host>:<port> to <n> recipient(s)`), `400` for a bad field, `502` naming only the host (an SMTP auth failure is `rejected the username or password`, never the server's reply). `Cache-Control: no-store` |
| `POST` | `/settings/backups/test` | `settingsHandler.TestBackupConnection` | Test the form's destination, admin only. Lists one object under the prefix (10 s limit), writes nothing, saves nothing. `200` or `502` with a redacted reason. `Cache-Control: no-store` |

---

## JSON API v1

`/api/v1/*` is the bearer-token JSON API used by the GoForge MCP tools. It is
documented in [API v1](api-v1.md).

## MCP

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `POST` | `/mcp` | `mcphttp.New` | MCP streamable HTTP (stateless, JSON responses). Bearer only; `full` tokens `403`. Off with `GOFORGE_MCP_HTTP=false` (`404`) |
| `GET`, `DELETE` | `/mcp` | `mcphttp.New` | `405` (no session, no server push) |

See [MCP server](../guides/mcp.md).

---

## Error Handlers

| Status | Handler | Description |
|--------|---------|-------------|
| 404 | `handlers.NotFoundHandler` | Custom not found page |
| 405 | `handlers.MethodNotAllowedHandler` | Method not allowed response |

Under `/api/v1` both answer JSON: `{"error":{"code":"not_found",...}}`.

---

## SSE (Server-Sent Events)

The following endpoints use Server-Sent Events for real-time streaming:

- **`/projects/{id}/deployments/{deploymentID}/logs`** - Streams deployment build and runtime logs in real-time
- **`/containers/{id}/stats`** - Streams CPU, memory, network I/O, and block I/O (`stats` event, first frame about 0.5 s after connect, then every 2 s), or an `unavailable` event when the container is stopped or missing
- **`/containers/{id}/logs`** - Streams container stdout/stderr output

SSE connections are long-lived HTTP connections. Clients should handle reconnection on disconnect.

!!! note "Server Timeout"
    SSE routes are excluded from the per-route `http.TimeoutHandler` middleware and have no write deadline, so they stream indefinitely until the client disconnects. All other routes enforce a 15-second timeout via middleware.

---

## CSRF Protection

All state-changing methods (`POST`, `PUT`, `DELETE`, `PATCH`) under the UI routes group require a valid CSRF token. The token can be provided via:

- **Header**: `X-CSRF-Token` (used by HTMX via `hx-headers`)
- **Form field**: `csrf_token` (used by standard HTML forms)

Signed in, the token is `HMAC(CSRF_SECRET, SHA-256(session token))`; signed out, it is a nonce plus its HMAC, kept in the `goforge_csrf` cookie (not HttpOnly). A cookie value without a valid MAC is rejected. See [Security](../security.md#csrf-protection).

`POST /auth/login` also answers `429` with `Retry-After` while the client IP or the account is locked out after repeated failures.

Webhook routes (`/webhooks/*`), the JSON API (`/api/v1/*`), and `/mcp` are
excluded from CSRF protection. `/api/v1` and `/mcp` accept only
`Authorization: Bearer` and ignore cookies, so a browser cannot be tricked
into calling them with a session.

---

## Source

Route definitions: `internal/web/router.go` (UI, webhooks) and
`internal/apiv1/handler.go` (`/api/v1`), and `internal/mcphttp` (`/mcp`)
