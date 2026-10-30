# Production Deployment

This section covers deploying GoForge to a production server.

## Deployment Options

| Method | Best For | Complexity |
|--------|----------|-----------|
| [Docker Compose](docker-compose.md) | Most deployments | Low |
| [Systemd](systemd.md) | Bare metal servers | Medium |

## Production Checklist

Before deploying GoForge to production, ensure:

### Security

- [ ] `GOFORGE_DEV_MODE=false`
- [ ] Strong, unique `ENCRYPTION_KEY` generated (`openssl rand -base64 32`)
- [ ] Strong, unique `SESSION_SECRET` generated
- [ ] Strong, unique `CSRF_SECRET` generated
- [ ] Strong database password (not the default `password`)
- [ ] `DATABASE_URL` uses `sslmode=require` or stronger
- [ ] GitHub OAuth callback URL uses HTTPS
- [ ] Traefik dashboard off (`TRAEFIK_DASHBOARD_INSECURE=false`, the default)
- [ ] Docker socket and `docker` group access treated as root (see `SECURITY.md`)

### Networking

- [ ] Domain DNS pointing to server
- [ ] Ports 80 and 443 open (for Let's Encrypt and HTTPS)
- [ ] `GOFORGE_BASE_URL` set to `https://your-domain.com`
- [ ] `GOFORGE_DOMAIN` set to `your-domain.com`
- [ ] `ACME_EMAIL` set to a valid email for Let's Encrypt

### Infrastructure

- [ ] Docker Engine installed and running
- [ ] Sufficient disk space for Docker images and volumes
- [ ] PostgreSQL database accessible
- [ ] Firewall configured (allow 80, 443; block direct access to 8080, 5432)

### Monitoring

- [ ] Log aggregation configured (journald, syslog, or Docker logging driver)
- [ ] Health check endpoint monitored (`/health`)
- [ ] Disk space monitoring (Docker images can accumulate)
- [ ] Nightly backups on (Settings → Profile → Backups, see [Backup & Restore](backup.md)) and `ENCRYPTION_KEY` stored off the server

## Resource Recommendations

| Component | CPU | RAM | Disk |
|-----------|-----|-----|------|
| GoForge | 1 core | 512 MB | 1 GB |
| PostgreSQL | 1 core | 1 GB | 10 GB |
| Traefik | 0.5 core | 256 MB | 100 MB |
| Docker Images/Containers | 2+ cores | 2+ GB | 20+ GB |
| **Total (minimum)** | **2 cores** | **4 GB** | **30+ GB** |

## Upgrading

### Docker Compose

```bash
git pull origin main
docker compose build goforge
docker compose up -d goforge
```

### Systemd

```bash
git pull origin main
make build
sudo systemctl stop goforge
sudo cp bin/goforge /opt/goforge/
sudo cp -r static /opt/goforge/
sudo systemctl start goforge
```

### Database Migrations

Migrations run automatically on startup. For manual control:

```bash
./bin/goforge migrate up
```

!!! tip "Zero-downtime upgrades"
    Run migrations separately before restarting the application:
    ```bash
    ./bin/goforge migrate up
    sudo systemctl restart goforge
    ```
