# Backup & Restore

Strategies for backing up and restoring GoForge data.

## What to Back Up

| Data | Location | Priority |
|------|----------|----------|
| PostgreSQL database | `postgres_data` volume | Critical |
| SSL certificates | `traefik_letsencrypt` volume | High |
| Environment config | `/opt/goforge/.env` | High |
| GoForge data | `goforge_data` volume | Medium |

!!! note
    Docker images and containers are not backed up -- they are rebuilt from source on deployment. Only the database and configuration need regular backups.

## Database Backup

### Using pg_dump (Recommended)

```bash
# Create a SQL dump
docker compose exec db pg_dump -U goforge goforge > backup_$(date +%Y%m%d_%H%M%S).sql

# Compressed backup
docker compose exec db pg_dump -U goforge goforge | gzip > backup_$(date +%Y%m%d).sql.gz
```

### Automated Backups with Cron

```bash
# Edit crontab
sudo crontab -e

# Daily backup at 2 AM
0 2 * * * cd /opt/goforge && docker compose exec -T db pg_dump -U goforge goforge | gzip > /backup/goforge_$(date +\%Y\%m\%d).sql.gz

# Weekly cleanup (keep 30 days)
0 3 * * 0 find /backup -name "goforge_*.sql.gz" -mtime +30 -delete
```

## Restore

### Database Restore

```bash
# Stop GoForge (optional but recommended)
docker compose stop goforge

# Restore from SQL dump
docker compose exec -T db psql -U goforge goforge < backup_20260101.sql

# Or from compressed backup
gunzip -c backup_20260101.sql.gz | docker compose exec -T db psql -U goforge goforge

# Restart GoForge
docker compose start goforge
```

### Full Restore to New Server

1. Install Docker and Docker Compose on the new server
2. Clone the GoForge repository
3. Copy your `.env` file
4. Start the database: `docker compose up -d db`
5. Restore the database backup
6. Start all services: `docker compose up -d`

## Volume Backups

For complete volume backups (including binary data):

```bash
# Stop services
docker compose stop

# Backup PostgreSQL volume
docker run --rm -v goforge_postgres_data:/source -v /backup:/dest \
  alpine tar czf /dest/postgres_data.tar.gz -C /source .

# Backup Traefik certificates
docker run --rm -v goforge_traefik_letsencrypt:/source -v /backup:/dest \
  alpine tar czf /dest/traefik_certs.tar.gz -C /source .

# Restart services
docker compose start
```

### Restore Volumes

```bash
docker compose stop

# Restore PostgreSQL volume
docker run --rm -v goforge_postgres_data:/dest -v /backup:/source \
  alpine sh -c "rm -rf /dest/* && tar xzf /source/postgres_data.tar.gz -C /dest"

docker compose start
```
