# Docker Compose Deployment

The recommended production deployment method using Docker Compose.

## Prerequisites

- Docker Engine 24+ with Docker Compose v2
- A domain name pointing to your server
- Ports 80 and 443 available

Compose publishes GoForge on `127.0.0.1:8080` for local access. Traefik on ports 80 and 443 is the public entrypoint. `.env.example` leaves `GOFORGE_DEV_MODE=false`.

## Setup

### 1. Clone and Configure

```bash
git clone https://github.com/raythurman2386/goforge.git /opt/goforge
cd /opt/goforge
cp .env.example .env
```

### 2. Edit Production Configuration

```bash
# /opt/goforge/.env

GOFORGE_DEV_MODE=false
GOFORGE_BASE_URL=https://goforge.example.com
GOFORGE_DOMAIN=goforge.example.com

# Generate these with: openssl rand -base64 32
ENCRYPTION_KEY=<generated>
SESSION_SECRET=<generated>
CSRF_SECRET=<generated>

# Strong database password
POSTGRES_PASSWORD=<strong-random-password>
DATABASE_URL=postgres://goforge:<strong-random-password>@db:5432/goforge?sslmode=disable

# Let's Encrypt
ACME_EMAIL=admin@example.com

# GitHub OAuth (optional)
GITHUB_CLIENT_ID=<your-id>
GITHUB_CLIENT_SECRET=<your-secret>
```

!!! note "Database SSL"
    `sslmode=disable` is acceptable when connecting to the PostgreSQL container over the Docker internal network. For external databases, use `sslmode=require`.

### 3. Start Services

```bash
docker compose up -d
```

### 4. Verify

```bash
# Check all services are running
docker compose ps

# Check logs
docker compose logs goforge
docker compose logs traefik

# Test HTTPS
curl -I https://goforge.example.com
```

## Service Management

```bash
# Start all services
docker compose up -d

# Stop all services
docker compose down

# Restart a specific service
docker compose restart goforge

# View logs
docker compose logs -f goforge

# View all logs
docker compose logs -f
```

## Updating

```bash
cd /opt/goforge
git pull origin main
docker compose build goforge
docker compose up -d goforge
```

## Backup

### Built-in nightly backups

Use the built-in backups for production. With `BACKUP_S3_BUCKET` and the other `BACKUP_*` variables set, `goforge serve` uploads the GoForge database and running Postgres, Gitea, and Vaultwarden services to S3-compatible storage every night (default `02:00 America/Chicago`) and prunes old runs. Run one now with:

```bash
docker compose exec goforge ./goforge backup run
```

Settings → Profile shows admins the last run. Restore is a manual runbook. See [Backup & Restore](backup.md). Keep a copy of `.env`, including `ENCRYPTION_KEY`, off the server; backups never include it.

### Manual database backup

For an ad-hoc copy outside the nightly run:

```bash
# Create backup
docker compose exec db pg_dump -U goforge goforge > backup_$(date +%Y%m%d).sql

# Restore
docker compose exec -T db psql -U goforge goforge < backup_20260101.sql
```

### Volume backup

Traefik certificates and raw volumes are not in the built-in backups. Copy them by hand if you need them:

```bash
# Stop services first for consistent backup
docker compose stop

# Backup PostgreSQL data
docker run --rm -v goforge_postgres_data:/data -v /backup:/backup \
  alpine tar czf /backup/postgres_$(date +%Y%m%d).tar.gz -C /data .

# Backup Traefik certificates
docker run --rm -v goforge_traefik_letsencrypt:/data -v /backup:/backup \
  alpine tar czf /backup/traefik_$(date +%Y%m%d).tar.gz -C /data .

docker compose start
```

## Security Hardening

### Firewall

```bash
# Allow only HTTP, HTTPS, and SSH
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

### Docker Socket

The GoForge container requires access to the Docker socket. This grants full Docker daemon access. Mitigations:

- Run on a dedicated server
- Use Docker socket proxy (like Tecnativa's docker-socket-proxy) to limit API access
- Monitor Docker audit logs

### Traefik Dashboard

Disable the dashboard in production or add authentication:

```bash
TRAEFIK_DASHBOARD_INSECURE=false
```
