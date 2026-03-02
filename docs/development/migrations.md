# Database Migrations

GoForge uses [golang-migrate](https://github.com/golang-migrate/migrate) for managing database schema changes.

## Creating a Migration

```bash
make migrate-create name=add_custom_domains
```

This creates two files with sequential numbering:

```
internal/database/migrations/postgres/
  012_add_custom_domains.up.sql
  012_add_custom_domains.down.sql
```

!!! note
    Existing migrations use sequential numeric prefixes (`001_`, `002_`, ..., `011_`), not timestamps.

## Writing Migrations

### Up Migration

The `up.sql` file applies the schema change:

```sql
-- internal/database/migrations/postgres/012_add_custom_domains.up.sql
CREATE TABLE IF NOT EXISTS custom_domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    domain VARCHAR(255) NOT NULL UNIQUE,
    ssl_status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (ssl_status IN ('pending', 'active', 'failed', 'expired')),
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_custom_domains_project ON custom_domains(project_id);
CREATE INDEX idx_custom_domains_domain ON custom_domains(domain);
```

### Down Migration

The `down.sql` file reverses the change:

```sql
-- internal/database/migrations/postgres/012_add_custom_domains.down.sql
DROP TABLE IF EXISTS custom_domains;
```

## Running Migrations

```bash
# Apply all pending migrations
make migrate

# Rollback the last migration
make migrate-down

# Check migration status
./bin/goforge migrate status
```

## Migration Best Practices

### Always write reversible migrations

Every `up.sql` must have a corresponding `down.sql` that cleanly reverses the change.

### Use `IF NOT EXISTS` / `IF EXISTS`

Makes migrations idempotent:

```sql
CREATE TABLE IF NOT EXISTS users (...);
DROP TABLE IF EXISTS users;
```

### Add proper constraints

```sql
-- Use CHECK constraints for enum-like columns
status VARCHAR(20) NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'active', 'failed'))

-- Use foreign keys with appropriate ON DELETE
project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE

-- Add indexes for frequently queried columns
CREATE INDEX idx_deployments_project ON deployments(project_id);
CREATE INDEX idx_deployments_status ON deployments(status);
```

### Avoid destructive changes

- Never rename columns directly -- add new, migrate data, drop old
- Never change column types without a migration strategy
- Never remove NOT NULL constraints without considering existing data

### Test both directions

```bash
# Apply
make migrate

# Verify
./bin/goforge migrate status

# Rollback
make migrate-down

# Re-apply
make migrate
```

## Current Schema

The database has 11 migration sets (001-011) creating tables for:

users, sessions, git_sources, projects, environments, env_variables, deployments, service_instances, containers, audit_logs, and a custom domain column on the projects table.

!!! note
    Migration 011 (`011_add_custom_domain_to_projects`) adds a column to the `projects` table rather than creating a separate `custom_domains` table.

See [Database Schema Reference](../reference/database-schema.md) for the complete schema.
