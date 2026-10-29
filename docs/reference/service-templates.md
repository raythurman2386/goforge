# Service Templates Reference

GoForge uses YAML template files to define one-click deployable services. Templates are stored in the `templates/` directory at the project root. GoForge reads them from `TEMPLATES_DIR` (default `./templates`) at startup and reloads them when files change. See [Adding Custom Service Templates](../guides/services.md#adding-custom-service-templates).

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
    type: string          # Field type: "text" (default), "password", "number", "bool", "select"
    default: string       # Default value (optional)
    required: boolean     # Whether field is required
    generate: boolean     # Auto-generate value (for passwords)
    options: [string]     # Allowed values (select only, required for select)

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

### Field types

| Type | Form control | Value in `{{ .Config.<name> }}` |
|------|--------------|----------------------------------|
| `text` | Text input. Also used when `type` is empty | As entered |
| `password` | Password input. With `generate: true`, a blank field gets a server-generated password | As entered or generated, stored encrypted |
| `number` | Number input | As entered |
| `bool` | Checkbox. `default: true` starts it checked | Always `true` or `false` |
| `select` | Drop-down of `options`. An optional select also offers a blank choice | One of `options`, or empty for an optional field left blank |

Use `bool` for on/off settings instead of a `text` field with a `"true" or "false"` hint, and `select` for a fixed set of values:

```yaml
config:
  - name: signups_allowed
    label: Allow Signups
    type: bool
    default: true
  - name: log_level
    label: Log Level
    type: select
    options: [info, debug, warn]
    default: info
    required: true
```

GoForge checks `bool` and `select` values on the server when you deploy a service and when you save **Edit configuration**, before any container is created or recreated. A `bool` accepts `true`/`false` (also `True`, `TRUE`, `1`/`0`, `t`/`f`) and is stored as `true` or `false`. A `select` value must match one of `options` exactly. Any other value is rejected with a `400` that names the field, for example `Allow Signups must be true or false, got "ture"`. A missing value takes the field's `default` (`false` for a `bool` without one).

A template is skipped at load, with an error in the log, if a `select` field has no `options`, a `select` default is not in `options`, or a `bool` default is not true or false.

---

## Template Variables

Templates use Go's `text/template` syntax. The following variables are available:

| Variable | Description |
|----------|-------------|
| `{{ .Version }}` | Selected version from the `versions` list |
| `{{ .Config.<name> }}` | User-provided configuration value by field name |
| `{{ .ContainerName }}` | Generated Docker container name |
| `{{ .Domain }}` | Domain entered on the configure form. Empty when none is set |
| `{{ .Protocol }}` | Scheme a browser uses for `Domain`: `https`, or `http` for an empty or `localhost` / `*.localhost` domain. Cloudflare proxy mode still gives `https` |

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

### SolidCraft

**File**: `templates/solidcraft.yaml`

| Property | Value |
|----------|-------|
| Name | `solidcraft` |
| Category | `cad` |
| Web | Yes (Domain field, Traefik route) |
| Versions | latest |
| Default version | latest |
| Default resources | 0.25 CPU, 128MB memory |
| Container port | 8080/tcp |

**Configuration fields**: none. Name and domain come from the configure form.

**Image**: `ghcr.io/tangency-labs/solidcraft-web:{{ .Version }}` (public, no registry login)

**Health check**: `wget --spider -q http://127.0.0.1:8080/`

**Connection**: internal `http://<container>:8080`, external `<protocol>://<domain>`

**Volume**: none. Project files stay in the browser's private storage, which needs HTTPS or `localhost`.

---

### Vaultwarden

**File**: `templates/vaultwarden.yaml`

| Property | Value |
|----------|-------|
| Name | `vaultwarden` |
| Category | `security` |
| Web | Yes (Domain field, Traefik route) |
| Versions | latest |
| Default version | latest |
| Default resources | 0.25 CPU, 256MB memory |
| Container port | 80/tcp |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `signups_allowed` | bool | `true` | Yes | `SIGNUPS_ALLOWED` at first start. Turn signups off later in `/admin` |
| `admin_token` | password | (generated, 48 chars) | Yes | `ADMIN_TOKEN` for `/admin` |

**Environment variables**:

```yaml
DOMAIN: "{{ if .Domain }}{{ .Protocol }}://{{ .Domain }}{{ end }}"
SIGNUPS_ALLOWED: "{{ .Config.signups_allowed }}"
ADMIN_TOKEN: "{{ .Config.admin_token }}"
```

WebSocket notifications are served on the main port by Vaultwarden 1.29 and later, so no `WEBSOCKET_ENABLED` or second port is needed.

**Health check**: `curl -fsS http://localhost:80/alive`

**Connection**: internal `http://<container>:80`, external `<protocol>://<domain>`

**Volume**: `data` -> `/data`

`latest` moves. To pin, add a version such as `1.37.4` (current `latest` on 2026-10-29) to `versions` or use an image digest.

---

### Gitea

**File**: `templates/gitea.yaml`

| Property | Value |
|----------|-------|
| Name | `gitea` |
| Category | `devtools` |
| Web | Yes (Domain field, Traefik route) |
| Versions | 1.25.5, latest |
| Default version | 1.25.5 |
| Default resources | 0.5 CPU, 512MB memory |
| Container ports | 3000/tcp (HTTP, routed by Traefik), 22/tcp (SSH, not routed or published) |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `app_name` | text | `Gitea: Git with a cup of tea` | Yes | `APP_NAME` |
| `disable_registration` | bool | `true` | Yes | `[service] DISABLE_REGISTRATION` |
| `require_signin_view` | bool | `true` | Yes | `[service] REQUIRE_SIGNIN_VIEW` |

**Environment variables**:

```yaml
USER_UID: "1000"
USER_GID: "1000"
GITEA____APP_NAME: "{{ .Config.app_name }}"
GITEA__SERVER__PROTOCOL: "http"
GITEA__SERVER__HTTP_PORT: "3000"
GITEA__SERVER__SSH_PORT: "22"
GITEA__SERVER__DOMAIN: "{{ if .Domain }}{{ .Domain }}{{ else }}{{ .ContainerName }}{{ end }}"
GITEA__SERVER__ROOT_URL: "{{ if .Domain }}{{ .Protocol }}://{{ .Domain }}/{{ else }}http://{{ .ContainerName }}:3000/{{ end }}"
GITEA__SERVICE__DISABLE_REGISTRATION: "{{ .Config.disable_registration }}"
GITEA__SERVICE__REQUIRE_SIGNIN_VIEW: "{{ .Config.require_signin_view }}"
```

Gitea maps `GITEA__<SECTION>__<KEY>` to `app.ini` on every start. The section name is case-insensitive. Keys in the root section need an empty section, so `APP_NAME` is `GITEA____APP_NAME` (four underscores). `GITEA__APP_NAME` has no key part and Gitea ignores it.

`PROTOCOL` stays `http` for every domain. TLS ends at Traefik or Cloudflare, and `PROTOCOL=https` would make Gitea expect TLS on port 3000. `ROOT_URL` uses `{{ .Protocol }}`, so a public domain such as `gitea-app.example.com` gets `https://gitea-app.example.com/`, including in Cloudflare proxy mode.

**Health check**: `wget --spider -q http://localhost:3000/api/healthz`

**Connection**: internal `http://<container>:3000`, external `<protocol>://<domain>`

**Volume**: `data` -> `/data`

The first visit opens Gitea's install page. Create the admin account there. See [One-Click Services](../guides/services.md#gitea).

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

- **image**: Prefer a pinned version tag or a digest (`name@sha256:...`) over `latest`. GoForge pulls tag references on every deploy and recreate, so a moved `latest` changes what runs the next time the container is created. Digest references are pulled only when missing. The **Update image** action on a service page pulls and recreates on demand
- **name**: Use lowercase, single word. Must be unique across all templates
- **versions**: List most recent first. Use string format even for numeric versions
- **password fields**: Set `generate: true` to auto-generate secure passwords
- **health_check**: Always include a health check for reliable startup detection
- **volumes**: Always use named volumes for data persistence
- **resources**: Set reasonable defaults; users cannot currently override these through the UI
