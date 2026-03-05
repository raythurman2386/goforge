# Database Configuration

GoForge uses PostgreSQL as its primary database. This page covers connection configuration, pooling, and migrations.

## Connection String

Set the `DATABASE_URL` environment variable to your PostgreSQL connection string:

```bash
# Format
DATABASE_URL=postgres://user:password@host:port/dbname?sslmode=MODE

# Local development
DATABASE_URL=postgres://goforge:password@localhost:5432/goforge?sslmode=disable

# Docker Compose (service name as host)
DATABASE_URL=postgres://goforge:password@db:5432/goforge?sslmode=disable

# Production
DATABASE_URL=postgres://goforge:STRONG_PASSWORD@db.example.com:5432/goforge?sslmode=require
```

### SSL Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `disable` | No SSL | Local development only |
| `require` | SSL required, no verification | Production (managed databases) |
| `verify-ca` | SSL + CA verification | High-security environments |
| `verify-full` | SSL + CA + hostname verification | Maximum security |

!!! warning
    Never use `sslmode=disable` in production. It transmits credentials and data in plaintext.

## Connection Pooling

GoForge configures the connection pool with these defaults:

| Setting | Value | Description |
|---------|-------|-------------|
| Max Open Connections | 25 | Maximum number of open database connections |
| Max Idle Connections | 5 | Maximum number of idle connections in the pool |
| Connection Max Lifetime | 5 minutes | Maximum time a connection can be reused |

These are set in `internal/database/database.go` and are not currently configurable via environment variables.

## Connection Retry

On startup, GoForge retries the database connection with increasing backoff:

- **Max retries:** 5
- **Delay between retries:** 1s, 2s, 3s, 4s, 5s (linear increase, `(i+1) * time.Second`)
- **Total maximum wait:** ~15 seconds

This handles cases where the database container starts slightly after GoForge (e.g., in Docker Compose).

## Migrations

GoForge uses [golang-migrate](https://github.com/golang-migrate/migrate) for schema management. Migrations are embedded in the binary and run automatically on startup.

### Automatic Migration

When GoForge starts via the `serve` command, it automatically runs `MigrateUp` to apply any pending migrations. This is convenient for development but should be considered carefully for production (see below).

### Manual Migration Commands

```bash
# Apply all pending migrations
make migrate
# or
./bin/goforge migrate up

# Rollback the last migration
make migrate-down
# or
./bin/goforge migrate down

# Check migration status
./bin/goforge migrate status

# Create a new migration
make migrate-create name=add_users_table
# or
./bin/goforge migrate create <name>
```

### Migration Files

Migration SQL files are located in `internal/database/migrations/postgres/`:

```
001_create_users.up.sql
001_create_users.down.sql
002_create_sessions.up.sql
002_create_sessions.down.sql
003_create_git_sources.up.sql
003_create_git_sources.down.sql
004_create_projects.up.sql
004_create_projects.down.sql
...
011_add_custom_domain_to_projects.up.sql
011_add_custom_domain_to_projects.down.sql
```

Each migration has an `up` (apply) and `down` (rollback) file.

### Production Migration Best Practices

!!! warning "Auto-migration in production"
    While convenient, auto-migration on startup has risks:
    
    - Multiple instances starting simultaneously may race on migrations
    - A bad migration prevents the application from starting
    - No manual review before schema changes are applied
    
    Consider running `goforge migrate up` as a separate step in your deployment pipeline, before starting the application.

## Schema Overview

GoForge creates these tables:

| Table | Purpose |
|-------|---------|
| `users` | User accounts and GitHub OAuth tokens |
| `sessions` | Authentication sessions with hashed tokens |
| `projects` | Project definitions and settings |
| `environments` | Deployment environments per project |
| `env_variables` | Environment-specific configuration |
| `deployments` | Deployment history and status |
| `deployment_logs` | Build and deploy log entries |
| `containers` | Running container metadata |
| `git_sources` | Git provider connections and credentials |
| `service_instances` | One-click service deployments |
| `audit_logs` | Administrative action audit trail |

!!! note
    Migration 011 adds a `custom_domain` column to the `projects` table to handle custom domains per project without requiring a separate table.

See the [Database Schema Reference](../reference/database-schema.md) for complete table definitions.
