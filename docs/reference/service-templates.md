# Service Templates Reference

GoForge uses YAML template files to define one-click deployable services. Templates are stored in the `templates/` directory at the project root.

---

## Template Schema

Each template YAML file follows this structure:

```yaml
# Identity
name: string              # Unique identifier (used in URLs and DB)
display_name: string      # Human-readable name
description: string       # Short description
icon: string              # Icon identifier for the UI
category: string          # Category (e.g., "database", "cache", "queue")

# Versioning
versions:                 # Available versions
  - "16"
  - "15"
default_version: "16"     # Default version if not specified

# User Configuration
config:                   # Configuration fields shown to the user
  - name: string          # Field identifier
    label: string         # Display label
    type: string          # Field type: "text", "password", "number"
    default: string       # Default value (optional)
    required: boolean     # Whether field is required
    generate: boolean     # Auto-generate value (for passwords)

# Resource Limits
resources:
  cpu: string             # CPU limit (Docker format, e.g., "0.5")
  memory: string          # Memory limit (Docker format, e.g., "512m")

# Docker Configuration
docker:
  image: string           # Docker image with version template
  ports:                  # Port mappings
    - container: integer  # Container port
      protocol: string    # Protocol (tcp/udp)
  environment:            # Environment variables (Go templates)
    KEY: "{{ .Config.fieldname }}"
  volumes:                # Persistent volumes
    - name: string        # Volume name suffix
      path: string        # Mount path in container
  health_check:           # Docker health check
    test: [string]        # Health check command
    interval: duration    # Check interval
    timeout: duration     # Check timeout
    retries: integer      # Retry count

# Connection Info
connection:
  internal: string        # Internal connection string template
```

---

## Template Variables

Templates use Go's `text/template` syntax. The following variables are available:

| Variable | Description |
|----------|-------------|
| `{{ .Version }}` | Selected version from the `versions` list |
| `{{ .Config.<name> }}` | User-provided configuration value by field name |
| `{{ .ContainerName }}` | Generated Docker container name |

---

## Available Templates

### PostgreSQL

**File**: `templates/postgres.yaml`

| Property | Value |
|----------|-------|
| Name | `postgres` |
| Category | `database` |
| Versions | 16, 15, 14 |
| Default version | 16 |
| Default resources | 0.5 CPU, 512MB memory |
| Container port | 5432/tcp |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `database` | text | `app` | Yes | Database name |
| `username` | text | `postgres` | Yes | Database username |
| `password` | password | (generated) | Yes | Database password |

**Environment variables**:

```yaml
POSTGRES_DB: "{{ .Config.database }}"
POSTGRES_USER: "{{ .Config.username }}"
POSTGRES_PASSWORD: "{{ .Config.password }}"
```

**Health check**: `pg_isready -U <username>`

**Connection string**: `postgres://<username>:<password>@<container>:5432/<database>`

**Volume**: `data` -> `/var/lib/postgresql/data`

---

### Redis

**File**: `templates/redis.yaml`

| Property | Value |
|----------|-------|
| Name | `redis` |
| Category | `database` |
| Versions | 7, 6 |
| Default version | 7 |
| Default resources | 0.25 CPU, 256MB memory |
| Container port | 6379/tcp |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `password` | password | (generated) | Yes | Redis password |

**Environment variables**:

```yaml
REDIS_PASSWORD: "{{ .Config.password }}"
```

**Health check**: `redis-cli ping | grep PONG`

**Connection string**: `redis://:<password>@<container>:6379`

**Volume**: `data` -> `/data`

---

## Creating Custom Templates

To add a new service template:

1. Create a YAML file in the `templates/` directory following the schema above
2. Restart GoForge (templates are loaded at startup from `internal/services/catalog.go`)
3. The service will appear in the catalog at `/services`

### Example: MySQL Template

```yaml
name: mysql
display_name: MySQL
description: The world's most popular open source database.
icon: mysql
category: database

versions:
  - "8.0"
  - "5.7"
default_version: "8.0"

config:
  - name: database
    label: Database Name
    type: text
    default: "app"
    required: true
  - name: root_password
    label: Root Password
    type: password
    generate: true
    required: true
  - name: username
    label: Username
    type: text
    default: "app"
    required: true
  - name: password
    label: Password
    type: password
    generate: true
    required: true

resources:
  cpu: "0.5"
  memory: "512m"

docker:
  image: "mysql:{{ .Version }}"
  ports:
    - container: 3306
      protocol: tcp
  environment:
    MYSQL_DATABASE: "{{ .Config.database }}"
    MYSQL_ROOT_PASSWORD: "{{ .Config.root_password }}"
    MYSQL_USER: "{{ .Config.username }}"
    MYSQL_PASSWORD: "{{ .Config.password }}"
  volumes:
    - name: data
      path: /var/lib/mysql
  health_check:
    test: ["CMD-SHELL", "mysqladmin ping -h localhost -u root -p{{ .Config.root_password }}"]
    interval: 10s
    timeout: 5s
    retries: 5

connection:
  internal: "mysql://{{ .Config.username }}:{{ .Config.password }}@{{ .ContainerName }}:3306/{{ .Config.database }}"
```

### Template Guidelines

- **name**: Use lowercase, single word. Must be unique across all templates
- **versions**: List most recent first. Use string format even for numeric versions
- **password fields**: Set `generate: true` to auto-generate secure passwords
- **health_check**: Always include a health check for reliable startup detection
- **volumes**: Always use named volumes for data persistence
- **resources**: Set reasonable defaults; users cannot currently override these through the UI
