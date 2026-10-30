# Docker Compose Setup

The recommended way to run GoForge with all its dependencies is using Docker Compose. This sets up GoForge, PostgreSQL, and Traefik. Cloudflare Tunnel is optional (`--profile cloudflare`). Postgres is only reachable on the Compose network.

!!! tip "Full Feature Testing"
    Use Docker Compose (not `make dev`) when you need to test features that require Docker integration:
    
    - Service deployment (PostgreSQL, Redis, etc.)
    - Project containerization and building
    - Complete end-to-end workflows

## Quick Start

```bash
# Clone the repository
git clone https://github.com/raythurman2386/goforge.git
cd goforge

# Configure environment
cp .env.example .env
# Edit .env with your settings (see below)

# Start all services
docker compose up -d
```

GoForge is published on `127.0.0.1:8080`, so `http://localhost:8080` works on the host and the port is not open on the public interface. Traefik on ports 80 and 443 is the entrypoint for direct installs (with Cloudflare Tunnel you can bind them to `127.0.0.1` in an override). The Traefik dashboard is off by default. The app process still listens on `0.0.0.0` inside the container so Traefik can reach it over the Docker network.

## Services Overview

The `docker-compose.yml` defines these services:

| Service | Description | Port |
|---------|-------------|------|
| `goforge` | Main application | 127.0.0.1:8080 |
| `db` | PostgreSQL 15 | none on the host (5432 inside the Compose network) |
| `traefik` | Reverse proxy with auto-SSL | 80, 443; dashboard off, 127.0.0.1:8081 only when `TRAEFIK_DASHBOARD_INSECURE=true` |
| `cloudflared` | Cloudflare Tunnel (optional, requires `--profile cloudflare`) | -- |

## Configuration

### Minimal Development Configuration

```bash
# .env
GOFORGE_DEV_MODE=true
DATABASE_URL=postgres://goforge:changeme@db:5432/goforge?sslmode=disable
POSTGRES_USER=goforge
POSTGRES_PASSWORD=changeme
POSTGRES_DB=goforge
ENCRYPTION_KEY=<openssl rand -base64 32>
SESSION_SECRET=<openssl rand -base64 32>
CSRF_SECRET=<openssl rand -base64 32>
```

Compose will not start until `DATABASE_URL`, `POSTGRES_PASSWORD`, `ENCRYPTION_KEY`, `SESSION_SECRET`, and `CSRF_SECRET` are set. The first account is a normal user. Set `GOFORGE_ADMIN_EMAIL` to that address and restart the `goforge` service to grant admin. Registration then stays closed unless `GOFORGE_ENABLE_REGISTRATION=true`.

### Production Configuration

For production, you must set these additional variables:

```bash
# .env
GOFORGE_DEV_MODE=false

# Your public domain
GOFORGE_BASE_URL=https://goforge.example.com
GOFORGE_DOMAIN=goforge.example.com

# Security secrets (generate with: openssl rand -base64 32)
ENCRYPTION_KEY=<generated-key>
SESSION_SECRET=<generated-secret>
CSRF_SECRET=<generated-secret>

# Strong database password
POSTGRES_PASSWORD=<strong-password>
DATABASE_URL=postgres://goforge:<strong-password>@db:5432/goforge?sslmode=disable

# SSL certificate email
ACME_EMAIL=admin@example.com

# GitHub OAuth (optional but recommended)
GITHUB_CLIENT_ID=<your-client-id>
GITHUB_CLIENT_SECRET=<your-client-secret>
```

## Enabling Cloudflare Tunnel

If you want to expose GoForge through Cloudflare Tunnel instead of direct port exposure:

```bash
# Set your tunnel token
CLOUDFLARE_TUNNEL_TOKEN=<your-token>
CLOUDFLARE_PROXY_MODE=true

# Start with the cloudflare profile
docker compose --profile cloudflare up -d
```

See [Cloudflare Tunnel Configuration](../configuration/cloudflare.md) for details.

## Volume Management

Docker Compose creates named volumes for persistent data:

| Volume | Purpose |
|--------|---------|
| `postgres_data` | PostgreSQL database files |
| `goforge_data` | GoForge application data |
| `traefik_letsencrypt` | Let's Encrypt certificates |
| `redis_data` | Redis persistence |

### Backups

GoForge has built-in nightly backups to S3-compatible storage (R2, B2, MinIO, AWS S3). They cover the GoForge database and running Postgres, Gitea, and Vaultwarden services. An admin turns them on in **Settings → Profile → Backups**; a save applies without a restart. Setting `BACKUP_S3_BUCKET`, `BACKUP_S3_ENDPOINT`, and the key pair in your env file (`.env`) still works: GoForge copies them into those settings on its first start. After that, remove every `BACKUP_*` line from your env file. `docker compose exec goforge ./goforge backup run` runs one backup now. Setup, what is covered, and the restore runbook: [Backup & Restore](../deployment/backup.md).

Back up your env file (`.env`), and `ENCRYPTION_KEY` in particular, somewhere off the server. Backups never include it.

For a one-off copy without a bucket:

```bash
# Backup PostgreSQL
docker compose exec db pg_dump -U goforge goforge > backup.sql

# Backup all volumes
docker run --rm -v goforge_postgres_data:/data -v $(pwd):/backup \
  alpine tar czf /backup/postgres_data.tar.gz -C /data .
```

### Restore from Backup

Restore is manual. For a built-in backup, follow [Backup & Restore](../deployment/backup.md#restore). For a one-off `pg_dump` from above:

```bash
# Restore PostgreSQL
docker compose exec -T db psql -U goforge goforge < backup.sql
```

## Updating GoForge

```bash
# Pull latest changes
git pull origin main

# Rebuild and restart
docker compose build goforge
docker compose up -d goforge
```

## Troubleshooting

### Database not ready

If GoForge starts before PostgreSQL is ready, it will retry the connection automatically. The Docker Compose configuration includes a health check dependency, but if issues persist:

```bash
# Check database status
docker compose ps db
docker compose logs db

# Restart GoForge after database is healthy
docker compose restart goforge
```

### Traefik certificate issues

```bash
# Check Traefik logs
docker compose logs traefik

# Verify the ACME email is set
echo $ACME_EMAIL

# Check certificate storage
docker compose exec traefik cat /letsencrypt/acme.json
```

### Container networking

All services communicate over the `goforge_net` Docker network. Ensure no port conflicts with other services on your host.

```bash
# Check network
docker network inspect goforge_goforge_net
```
