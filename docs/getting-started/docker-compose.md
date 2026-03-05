# Docker Compose Setup

The recommended way to run GoForge with all its dependencies is using Docker Compose. This sets up GoForge, PostgreSQL, Traefik, and optionally Cloudflare Tunnel and Redis in a single command.

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

GoForge will be available at `http://localhost:8080`.

## Services Overview

The `docker-compose.yml` defines these services:

| Service | Description | Port |
|---------|-------------|------|
| `goforge` | Main application | 8080 |
| `db` | PostgreSQL 15 | 5432 (exposed to host) |
| `traefik` | Reverse proxy with auto-SSL | 80, 443 |
| `cloudflared` | Cloudflare Tunnel (optional, requires `--profile cloudflare`) | -- |
| `redis` | Redis cache (starts by default) | 6379 |

## Configuration

### Minimal Development Configuration

```bash
# .env
GOFORGE_DEV_MODE=true
DATABASE_URL=postgres://goforge:password@db:5432/goforge?sslmode=disable
POSTGRES_USER=goforge
POSTGRES_PASSWORD=password
POSTGRES_DB=goforge
```

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

### Backup Volumes

```bash
# Backup PostgreSQL
docker compose exec db pg_dump -U goforge goforge > backup.sql

# Backup all volumes
docker run --rm -v goforge_postgres_data:/data -v $(pwd):/backup \
  alpine tar czf /backup/postgres_data.tar.gz -C /data .
```

### Restore from Backup

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
