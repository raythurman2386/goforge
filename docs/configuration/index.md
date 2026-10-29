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
| `CSRF_SECRET` | Auto-generated | Secret for CSRF token generation |

Generate production secrets with:

```bash
openssl rand -base64 32
```

!!! danger "Required in production"
    When `GOFORGE_DEV_MODE=false`, `ENCRYPTION_KEY`, `SESSION_SECRET`, and `CSRF_SECRET` must be explicitly set. The application will refuse to start without them. Docker Compose also refuses to start while those variables, `DATABASE_URL`, or `POSTGRES_PASSWORD` are empty.

Dev mode writes any secrets it generates to `GOFORGE_DEV_SECRETS_FILE` (mode `0600`) and reloads them on the next start. Values already set in the environment are used as-is and are not copied into that file.

## API v1 and MCP

| Variable | Default | Description |
|----------|---------|-------------|
| `GOFORGE_PROTECTED_SERVICES` | empty | Comma-separated service instance IDs. Through `/api/v1` (and so the MCP server), stopping, reconfiguring, or re-imaging one of them needs a token with the `protected` scope; without it the API answers `403 protected_resource` before any Docker call. Restart is still allowed with `write`. The UI is not affected |

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
| `ACME_EMAIL` | (empty in app code; `admin@example.com` in docker-compose.yml) | Email for Let's Encrypt certificate registration |

!!! note "Compose-only variables"
    `TRAEFIK_DASHBOARD_PORT`, `TRAEFIK_DASHBOARD_INSECURE`, and `TRAEFIK_LOG_LEVEL` are used only in `docker-compose.yml` and are not part of GoForge's Go application configuration.

See [Traefik & SSL Configuration](traefik.md) for details.

## Cloudflare Tunnel

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDFLARE_PROXY_MODE` | `false` | Enable Cloudflare proxy mode (HTTP entrypoint, skip Let's Encrypt) |

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
| `WEBHOOK_SECRET` | (empty) | Shared secret for webhook signature verification |

## Deployment Limits

| Variable | Default | Description |
|----------|---------|-------------|
| `MAX_CONCURRENT_BUILDS` | `2` | Accepted, but unused. The worker runs one deployment at a time |
| `BUILD_TIMEOUT` | `15m` | Maximum time for a single build (Go duration format) |

## Monitoring

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_METRICS` | `true` | Enable Prometheus metrics endpoint |
| `METRICS_RETENTION` | `168h` | How long to retain container metrics (default: 7 days) |

## Backups

Nightly backups to S3-compatible storage. Off while `BACKUP_S3_BUCKET` is empty. See [Backup & Restore](../deployment/backup.md).

| Variable | Default | Description |
|----------|---------|-------------|
| `BACKUP_S3_ENDPOINT` | (empty) | Endpoint URL, `http` or `https`, no credentials or path. Required with a bucket. R2: `https://<account id>.r2.cloudflarestorage.com` |
| `BACKUP_S3_REGION` | `auto` | Region. `auto` works for R2 |
| `BACKUP_S3_BUCKET` | (empty) | Bucket. Setting it turns backups on |
| `BACKUP_S3_PREFIX` | `goforge/` | Key prefix inside the bucket. A trailing `/` is added. No `..` or `\` |
| `BACKUP_S3_ACCESS_KEY_ID` | (empty) | Access key. Required with a bucket |
| `BACKUP_S3_SECRET_ACCESS_KEY` | (empty) | Secret key. Required with a bucket. Redacted from errors and logs |
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
