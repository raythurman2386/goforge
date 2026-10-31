# Environment Variables

GoForge is configured entirely through environment variables. This page documents every available variable with its default value and purpose.

!!! tip "Development Mode"
    Set `GOFORGE_DEV_MODE=true` to auto-generate security secrets and enable debug logging. This is the recommended configuration for local development.

## Server Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `GOFORGE_DEV_MODE` | `false` | Enable development mode (debug logging; generates secrets when they are unset) |
| `GOFORGE_DEV_SECRETS_FILE` | `.goforge/dev-secrets.json` | Dev-mode file that keeps generated secrets across restarts |
| `GOFORGE_ENABLE_REGISTRATION` | `false` | Allow accounts after the first one. The first account can always register. New accounts are users |
| `GOFORGE_ADMIN_EMAIL` | empty | Promote this email to admin on startup and when the account is created. Matching is case-insensitive |
| `GOFORGE_HOST` | `0.0.0.0` | Host address to bind to |
| `GOFORGE_PORT` | `8080` | Port to listen on |
| `GOFORGE_BASE_URL` | `http://localhost:8080` | Public-facing base URL (used for OAuth callbacks, webhooks) |

!!! note "Compose-only variable"
    `GOFORGE_DOMAIN` is used in `docker-compose.yml` for Traefik routing labels but is not part of GoForge's Go application configuration.

## Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://goforge:password@localhost:5432/goforge?sslmode=disable` | PostgreSQL connection string |
| `POSTGRES_USER` | `goforge` | PostgreSQL username (for Docker Compose) |
| `POSTGRES_PASSWORD` | `password` | PostgreSQL password (for Docker Compose) |
| `POSTGRES_DB` | `goforge` | PostgreSQL database name (for Docker Compose) |

!!! warning "Production database"
    Always use a strong password and enable `sslmode=require` in production. The default `sslmode=disable` is only suitable for local development.

See [Database Configuration](database.md) for connection pooling and migration details.

## Security

| Variable | Default (dev) | Description |
|----------|---------------|-------------|
| `ENCRYPTION_KEY` | Auto-generated | 32-byte base64-encoded key for AES-256-GCM encryption of secrets |
| `SESSION_SECRET` | Auto-generated | Secret for session cookie signing |
| `CSRF_SECRET` | Auto-generated | HMAC key for CSRF tokens. Tokens are bound to the session and a planted `goforge_csrf` cookie does not verify without it. Changing it invalidates open forms (reload the page) |

Generate production secrets with:

```bash
openssl rand -base64 32
```

!!! danger "Required in production"
    When `GOFORGE_DEV_MODE=false`, `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, and `WEBHOOK_SECRET` must be explicitly set. The application will refuse to start without them. Docker Compose also refuses to start while those variables, `DATABASE_URL`, or `POSTGRES_PASSWORD` are empty.

Dev mode writes any secrets it generates to `GOFORGE_DEV_SECRETS_FILE` (mode `0600`) and reloads them on the next start. Values already set in the environment are used as-is and are not copied into that file.

## API v1 and MCP

| Variable | Default | Description |
|----------|---------|-------------|
| `GOFORGE_PROTECTED_SERVICES` | empty | Comma-separated service instance IDs. Through `/api/v1` (and so the MCP server), stopping, reconfiguring, or re-imaging one of them needs a token with the `protected` scope; without it the API answers `403 protected_resource` before any Docker call. Restart is still allowed with `write`. The UI is not affected |
| `GOFORGE_MCP_HTTP` | `true` | Serves the MCP tools at `/mcp` for scoped Profile API tokens. `false` removes the route (`404`) and hides the Profile MCP card; `/api/v1` and `cmd/goforge-mcp` are not affected |

See [API v1](../reference/api-v1.md) and [MCP server](../guides/mcp.md).

## GitHub OAuth

| Variable | Default | Description |
|----------|---------|-------------|
| `GITHUB_CLIENT_ID` | (empty) | GitHub OAuth App client ID |
| `GITHUB_CLIENT_SECRET` | (empty) | GitHub OAuth App client secret |

See [Authentication Configuration](authentication.md) for setup instructions.

## Docker

| Variable | Default | Description |
|----------|---------|-------------|
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker daemon socket or TCP address |

## Traefik / Reverse Proxy

| Variable | Default | Description |
|----------|---------|-------------|
| `TRAEFIK_NETWORK` | `traefik` | Docker network for Traefik service discovery |
| `TRUSTED_PROXIES` | private and loopback ranges | Comma-separated CIDRs or IPs of reverse proxies whose client-IP headers are read. Only from such a peer does GoForge use `CF-Connecting-IP` (only with `CLOUDFLARE_PROXY_MODE=true`), then the rightmost `X-Forwarded-For` hop that is not in this list, then `X-Real-IP`. The client IP keys the rate limiter, the login lockout, and audit rows. Traefik must stay in this list (for example `172.18.0.0/16`) |
| `ACME_EMAIL` | (empty in app code; `admin@example.com` in docker-compose.yml) | Email for Let's Encrypt certificate registration |

!!! note "Compose-only variables"
    `TRAEFIK_DASHBOARD_PORT`, `TRAEFIK_DASHBOARD_INSECURE`, and `TRAEFIK_LOG_LEVEL` are used only in `docker-compose.yml` and are not part of GoForge's Go application configuration. `TRAEFIK_DASHBOARD_INSECURE` (default `false`) turns the Traefik API and dashboard on, unauthenticated, on `127.0.0.1:${TRAEFIK_DASHBOARD_PORT}` only.

See [Traefik & SSL Configuration](traefik.md) for details.

## Cloudflare Tunnel

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDFLARE_PROXY_MODE` | `false` | Enable Cloudflare proxy mode (HTTP entrypoint, skip Let's Encrypt, read the client IP from `CF-Connecting-IP`). Only enable it when every request reaches Traefik through Cloudflare: Traefik forwards `CF-Connecting-IP` as sent |

!!! note "Compose-only variable"
    `CLOUDFLARE_TUNNEL_TOKEN` is used only in `docker-compose.yml` for the cloudflared container.

See [Cloudflare Tunnel Configuration](cloudflare.md) for setup instructions.

## Service Templates

| Variable | Default | Description |
|----------|---------|-------------|
| `TEMPLATES_DIR` | `./templates` | Directory containing service template YAML files. Changes are picked up without a restart; bind-mount it to add templates without rebuilding the image (see [Adding Custom Service Templates](../guides/services.md#adding-custom-service-templates)) |
| `GOFORGE_BUILD_DIR` | System temp dir | Directory for cloning repositories during builds |

## Webhooks

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBHOOK_SECRET` | (empty) | Shared secret for webhook signature (GitHub, Gitea) and token (GitLab) checks. Required outside dev mode: GoForge refuses to start while it is empty, because an empty secret lets anyone forge a webhook. Dev mode generates it |

## Deployment Limits

| Variable | Default | Description |
|----------|---------|-------------|
| `MAX_CONCURRENT_BUILDS` | `2` | Accepted, but unused. The worker runs one deployment at a time |
| `BUILD_TIMEOUT` | `15m` | Maximum time for a single build (Go duration format) |

## Monitoring

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_METRICS` | `true` | Accepted, but unused. There is no `/metrics` endpoint |
| `METRICS_RETENTION` | `168h` | Accepted, but unused. Container metrics are streamed live and not stored |

## Backups

Nightly backups to S3-compatible storage. An admin sets them up in **Settings → Profile → Backups**, and a save applies without a restart. With Docker Compose this is the way to set them up: `docker-compose.yml` does not pass any `BACKUP_*` variable into the `goforge` container, so `BACKUP_*` in your env file (`.env`) has no effect there.

The variables below are read from the process environment. Use them when you run the binary on the host, or for a one-time import on a compose install by adding them under `services.goforge.environment` in your own `docker-compose.override.yml` (see [Backup & Restore](../deployment/backup.md#from-the-environment-host-runs-or-an-override)). With no saved settings and `BACKUP_S3_BUCKET` set, GoForge copies them into the saved settings on its first start. Once saved settings exist they win, and later changes to these variables are ignored with a warning in the log; an incomplete or invalid `BACKUP_*` set no longer stops startup then. With no saved settings, an invalid set stops GoForge at startup. After the import, remove every `BACKUP_*` variable again. With neither, backups are off. `goforge backup run` uses the same settings. See [Backup & Restore](../deployment/backup.md#turn-it-on).

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKUP_S3_ENDPOINT` | (empty) | Endpoint URL, `http` or `https`, no credentials or path. Required with a bucket. R2: `https://<account id>.r2.cloudflarestorage.com` |
| `BACKUP_S3_REGION` | `auto` | Region. `auto` works for R2 |
| `BACKUP_S3_BUCKET` | (empty) | Bucket. Setting it turns backups on, until saved settings exist |
| `BACKUP_S3_PREFIX` | `goforge/` | Key prefix inside the bucket. A trailing `/` is added. No `..` or `\` |
| `BACKUP_S3_ACCESS_KEY_ID` | (empty) | Access key. Required with a bucket |
| `BACKUP_S3_SECRET_ACCESS_KEY` | (empty) | Secret key. Required with a bucket. Redacted from errors and logs. Saved settings store it encrypted with `ENCRYPTION_KEY` |
| `BACKUP_S3_FORCE_PATH_STYLE` | `false` | Path-style requests. Set `true` for MinIO |
| `BACKUP_SCHEDULE` | `02:00 America/Chicago` | Daily time as `HH:MM Zone/Name`, or `off` to run only with `goforge backup run` |
| `BACKUP_RETENTION_DAILY` | `7` | Complete days kept (1-366) |
| `BACKUP_TIMEOUT` | `2h` | Upper limit for one run (Go duration) |

!!! danger "ENCRYPTION_KEY is not backed up"
    The database dump holds secrets encrypted with `ENCRYPTION_KEY`. The key is never written to a backup. Keep a copy of it outside the server.

## Logging

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` (dev: `debug`) | Log level: debug, info, warn, error |
| `LOG_FORMAT` | `json` (dev: `text`) | Log format: json, text |
