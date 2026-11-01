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
    length: integer       # Generated length (minimum 16, default 24)
    immutable: boolean    # Set once at deploy, never changed by Edit configuration
    options: [string]     # Allowed values (select only, required for select)

# Resource Limits
resources:
  cpu: string             # CPU limit (Docker format, e.g., "0.5")
  memory: string          # Memory limit (Docker format, e.g., "512m")

# Docker Configuration
docker:
  image: string           # Docker image with version template
  ports:                  # Container ports. The first is the HTTP port Traefik routes to
    - container: integer  # Container port
      protocol: string    # Protocol (tcp/udp)
      publish: bool       # Publish on the host (optional, default false)
      host_port: string   # Host port, Go template (required with publish), e.g. "{{ .Config.ssh_port }}"
      host_ip: string     # Host address (optional, default 127.0.0.1). Loopback only; 0.0.0.0 and :: are refused
  environment:            # Environment variables (Go templates)
    KEY: "{{ .Config.fieldname }}"
  optional_environment:   # Like environment, but left out when the value renders empty
    KEY: "{{ .Config.fieldname }}"
  command: [string]       # Replaces the image CMD (optional, Go templates). Never template a secret into it
  entrypoint: [string]    # Replaces the image ENTRYPOINT and drops its CMD (optional, Go templates). Same secret rule as command
  volumes:                # Persistent volumes
    - name: string        # Volume name suffix
      path: string        # Mount path in container
  health_check:           # Docker health check
    test: [string]        # Health check command. Not a Go template; read config from the container env
    interval: duration    # Check interval
    timeout: duration     # Check timeout
    retries: integer      # Retry count

# Post-start hooks (optional)
post_start:               # Commands run with docker exec once the container is healthy
  - name: string          # Hook name, used in logs
    user: string          # User to run as (optional)
    command: [string]     # Command and arguments (Go templates)
    env:                  # Extra environment for the command only (Go templates)
      KEY: "{{ .Config.fieldname }}"

# Connection Info
connection:
  internal: string        # Internal connection string template
```

### Immutable fields

A field with `immutable: true` gets its value at deploy (generated there when `generate: true` and left blank) and keeps it. **Edit configuration** does not show it and ignores it if a form sends it, and an edit never generates it. An instance deployed before the field was added to the template stays without it. Use it for values a service writes into its own data on first start and that must not change later, such as an encryption key or the first admin password.

Pair an immutable field with `optional_environment` when an empty value must not reach the container. GoForge leaves a variable in `optional_environment` out when it renders to an empty string, so an older instance without the field keeps whatever the service already stored. A field that the template declares but the stored config lacks renders as its `default`, or as an empty string when it has none, never as `<no value>`.

### Published ports

GoForge never publishes a container port on the host unless the template says so with `publish: true`. Traefik routes only the first port, over HTTP; publish a port for a protocol Traefik does not carry, such as Gitea's SSH.

```yaml
docker:
  ports:
    - container: 3000          # HTTP, routed by Traefik
      protocol: tcp
    - container: 22            # SSH, published on 127.0.0.1:<ssh_port>
      protocol: tcp
      publish: true
      host_port: "{{ .Config.ssh_port }}"
      host_ip: "127.0.0.1"
```

- `host_port` is required with `publish` and is a Go template with the same variables as `environment`. It must render to a port from 1 to 65535; anything else rejects the deploy or edit before a container is touched.
- `host_ip` defaults to `127.0.0.1`. It must be a loopback address (`127.0.0.1`, `::1`). `0.0.0.0`, `::`, and every other address are refused when the catalog loads, because Docker publishes bypass the host firewall. Reach a loopback port from elsewhere with an SSH jump to the host.
- `host_port` or `host_ip` without `publish: true` is an error.
- The binding is stored on the container record and shown on the service page under **Connection Details** (`published 22/tcp` → `127.0.0.1:2223`).
- Two instances with the same host port collide. When **Edit configuration** or **Update image** recreates a container that publishes a port, GoForge renames the old container aside instead of removing it, and removes it only once the new one runs. If the new container cannot start (for example, its host port is taken), the old container gets its name back and starts again, and the instance is marked `failed`.

### Post-start hooks

`post_start` runs one-time setup that needs the service up, such as creating the first admin account. After GoForge creates and starts the container (on deploy, **Edit configuration**, and **Update image**), it waits until the health check reports healthy, or until the container runs when there is no health check. Then it runs each hook in order with `docker exec`, as `user` when set. `command` and `env` values are Go templates with the same variables as `environment`. The command also sees the container's own environment.

- The instance turns `running` only when every hook exits 0. If the container turns unhealthy or stops first, or a hook exits non-zero, GoForge stops there and marks the instance `failed`. The container and its volumes are left in place.
- GoForge logs each hook's name, exit code, and duration. It never logs `env`. For a failed hook it logs the end of the output with every secret config value and every hook `env` value masked.
- Pass secrets and user-entered values in `env`, not in `command`, and reference them as quoted variables (`"$ADMIN_PASSWORD"`) inside the script. GoForge masks `env` values in logs, but a value templated into `command` is part of the exec request itself.
- The exec `env` is not in the container's own environment. A program the hook runs, though, is an ordinary process: if the script passes a secret to it as an argument, as `gitea admin user create --password` requires, that argument is in the process's command line for as long as the program runs. Container processes show up in the host's process table, so on a host where `/proc` is not mounted with `hidepid`, any local user can read it there while the command runs. Prefer tools that read secrets from the environment, a file, or stdin, and keep any command that takes one as an argument short.
- Hooks run again on every recreate, so make each one idempotent: check whether the work is already done and exit 0.
- A hook that never exits is stopped when the deploy, edit, or update deadline (10 minutes) passes, and the instance turns `failed`.
- A template is skipped at load if a hook has no `command`. Templates without `post_start` behave exactly as before: no health wait and no exec.

```yaml
post_start:
  - name: create-admin
    user: git
    command: ["sh", "-c", "gitea admin user list --admin | tail -n +2 | grep -q . || gitea admin user create --admin --username \"$ADMIN_USER\" --email \"$ADMIN_EMAIL\" --password \"$ADMIN_PASSWORD\""]
    env:
      ADMIN_USER: "{{ .Config.admin_username }}"
      ADMIN_EMAIL: "admin@{{ .Domain }}"
      ADMIN_PASSWORD: "{{ .Config.admin_password }}"
```

### Field types

| Type | Form control | Value in `{{ .Config.<name> }}` |
|------|--------------|----------------------------------|
| `text` | Text input. Also used when `type` is empty | As entered |
| `password` | Password input. With `generate: true`, a blank field gets a server-generated password | As entered or generated, stored encrypted |
| `number` | Number input | As entered |
| `bool` | Checkbox. `default: true` starts it checked | Always `true` or `false` |
| `select` | Drop-down of `options`. An optional select also offers a blank choice | One of `options`, or, for an optional field left blank, the field's `default` if one is set (empty only when there is no default) |

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
| Versions | 17, 16, 15, 14 |
| Default version | 17 |
| Default resources | 1 CPU, 1GiB memory |
| Container port | 5432/tcp |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `database` | text, immutable | `app` | Yes | Database created at first start |
| `username` | text, immutable | `pgadmin` | Yes | Admin (superuser) role created at first start |
| `password` | password, immutable | (generated, 32 chars) | Yes | Admin password |

The image applies `POSTGRES_DB`, `POSTGRES_USER` and `POSTGRES_PASSWORD` only when it initialises an empty volume, so all three are immutable: set at deploy, never changed by **Edit configuration**. To rotate the admin password, run `ALTER ROLE` inside Postgres and update your clients; the stored value then no longer matches. Instances deployed before these fields were immutable keep their stored values.

A major version change (for example 16 to 17) needs a dump and restore; a newer server does not start on an older data directory.

**Environment variables**:

```yaml
POSTGRES_DB: "{{ .Config.database }}"
POSTGRES_USER: "{{ .Config.username }}"
POSTGRES_PASSWORD: "{{ .Config.password }}"
```

**Health check**: `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`

**Connection string**: `postgres://<username>:<password>@<container>:5432/<database>`

**Volume**: `data` -> `/var/lib/postgresql/data`

**App roles**: `scripts/shared-data/pg-tenant.sh <container> <app>` creates or updates one app's `LOGIN` role and database (owned by the role, `CONNECT` revoked from `PUBLIC`), reading the password from stdin. It is idempotent and rotates the password when it changes. See [Shared data services](../guides/services.md#shared-data-services).

---

### Redis

**File**: `templates/redis.yaml`

| Property | Value |
|----------|-------|
| Name | `redis` |
| Category | `database` |
| Versions | 7.4, 7, 6 |
| Default version | 7.4 |
| Default resources | 0.25 CPU, 512MiB memory (Redis `maxmemory` is 256mb) |
| Container port | 6379/tcp |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `password` | password | (generated) | Yes | Redis password |

**Environment variables**:

```yaml
REDIS_PASSWORD: "{{ .Config.password }}"
```

**Command**: the official `redis` image ignores `REDIS_PASSWORD`, so the template's `command` writes the password into the ACL file `/data/users.acl` as the `default` user's SHA-256 hash (`user default on #<sha256> ~* &* +@all`), writes `/tmp/goforge-redis.conf` (mode 600, inside the container, not in the volume) with `aclfile /data/users.acl`, and runs `docker-entrypoint.sh redis-server /tmp/goforge-redis.conf`. There is no `requirepass`: on 7.4, loading an ACL file resets the `default` user to `nopass`, so `requirepass` next to `aclfile` turns auth off; the ACL file has to own the password. The password is read from the environment, so it is not in the container command, the host process list, or either file. An empty `REDIS_PASSWORD` stops the container before Redis starts. A client that does not `AUTH` gets `NOAUTH`.

Both files are rewritten on every container start (restart, stop/start, daemon reboot). The `default` line is always regenerated from `REDIS_PASSWORD`, so an edited or stale default user cannot outlive a restart; every other line of `users.acl` (per-app users saved by `scripts/shared-data/redis-tenant.sh`) is kept. `users.acl` is built as `/data/.users.acl.next` (mode 600, owned by `redis`) and renamed into place. Keep the `rm -f` before writing the conf file: `/tmp` is sticky and `fs.protected_regular=1` makes root's `>` into the old redis-owned file fail with `Permission denied`, so a second start would exit 2 (Gatekeeper P8).

**Persistence and memory**: Redis is set up for sessions and queues, not as a cache. The conf sets `dir /data`, `appendonly yes`, `appendfsync everysec` (AOF files in `/data/appendonlydir`, alongside RDB snapshots), `maxmemory 256mb`, and `maxmemory-policy noeviction`: a full instance answers writes with `OOM` instead of dropping keys. The 512MiB container limit leaves room for client buffers and the fork of an AOF rewrite.

**Health check**: `REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli ping | grep PONG`

**Connection string**: `redis://:<password>@<container>:6379`

**Volume**: `data` -> `/data` (data, AOF files, and `users.acl`)

**App users**: `scripts/shared-data/redis-tenant.sh <container> <app>` creates or updates one app's ACL user, confined to keys and Pub/Sub channels matching `<app>:*` (`SCAN`, `RANDOMKEY`, `PUBSUB`, and on 7.x `FUNCTION` denied), reading the password from stdin; `--revoke` deletes it. Changes are saved with `ACL SAVE`, so they survive restarts. On an instance without an ACL file (before #60) it exits 1 and changes nothing. See [Shared data services](../guides/services.md#shared-data-services).

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
ADMIN_TOKEN: "{{ argon2id .Config.admin_token }}"
```

The container gets an Argon2id PHC hash of the token, so Vaultwarden does not warn about a plain `ADMIN_TOKEN`. Sign in to `/admin` with the plain token from the service page.

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
| Container ports | 3000/tcp (HTTP, routed by Traefik), 22/tcp (SSH, published on `127.0.0.1:<ssh_port>`) |

**Configuration fields**:

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `app_name` | text | `Gitea: Git with a cup of tea` | Yes | `APP_NAME` |
| `disable_registration` | bool | `true` | Yes | `[service] DISABLE_REGISTRATION` |
| `require_signin_view` | bool | `true` | Yes | `[service] REQUIRE_SIGNIN_VIEW` |
| `admin_username` | text | `gitea-admin` | Yes | First admin account (`admin` is reserved by Gitea) |
| `admin_email` | text | blank | No | First admin email. Blank uses `admin@<Domain>`, or `admin@<container name>` without a Domain |
| `admin_password` | password, immutable | (generated, 32 chars) | Yes | First admin password, masked on the service page |
| `secret_key` | password, immutable | (generated, 64 chars) | Yes | `[security] SECRET_KEY` |
| `ssh_port` | number | `2223` | Yes | Host port SSH is published on (`127.0.0.1` only) and `[server] SSH_PORT` |
| `ssh_domain` | text | `minis` | No | `[server] SSH_DOMAIN`, the host in SSH clone URLs. Blank uses `DOMAIN` |

**Environment variables**:

```yaml
USER_UID: "1000"
USER_GID: "1000"
GITEA____APP_NAME: "{{ .Config.app_name }}"
GITEA__SERVER__PROTOCOL: "http"
GITEA__SERVER__HTTP_PORT: "3000"
GITEA__SERVER__SSH_PORT: "{{ .Config.ssh_port }}"
GITEA__SERVER__DOMAIN: "{{ if .Domain }}{{ .Domain }}{{ else }}{{ .ContainerName }}{{ end }}"
GITEA__SERVER__ROOT_URL: "{{ if .Domain }}{{ .Protocol }}://{{ .Domain }}/{{ else }}http://{{ .ContainerName }}:3000/{{ end }}"
GITEA__SERVICE__DISABLE_REGISTRATION: "{{ .Config.disable_registration }}"
GITEA__SERVICE__REQUIRE_SIGNIN_VIEW: "{{ .Config.require_signin_view }}"
GITEA__security__INSTALL_LOCK: "true"
# optional_environment: left out when empty (instances deployed before secret_key existed)
GITEA__security__SECRET_KEY: "{{ .Config.secret_key }}"
# optional_environment: left out when blank, so Gitea uses DOMAIN
GITEA__SERVER__SSH_DOMAIN: "{{ .Config.ssh_domain }}"
```

Gitea maps `GITEA__<SECTION>__<KEY>` to `app.ini` on every start. The section name is case-insensitive. Keys in the root section need an empty section, so `APP_NAME` is `GITEA____APP_NAME` (four underscores). `GITEA__APP_NAME` has no key part and Gitea ignores it.

`SSH_PORT` is the published host port, not container port 22: Gitea's OpenSSH still listens on 22 inside the container, and `SSH_PORT` only sets the port in clone URLs. An instance deployed before `ssh_port` and `ssh_domain` existed renders `2223` and `minis` until you edit them.

`PROTOCOL` stays `http` for every domain. TLS ends at Traefik or Cloudflare, and `PROTOCOL=https` would make Gitea expect TLS on port 3000. `ROOT_URL` uses `{{ .Protocol }}`, so a public domain such as `gitea-app.example.com` gets `https://gitea-app.example.com/`, including in Cloudflare proxy mode.

**Health check**: `wget --spider -q http://localhost:3000/api/healthz`

**Connection**: internal `http://<container>:3000`, external `<protocol>://<domain>`

**Volume**: `data` -> `/data`

**Post-start**: `create-admin` runs as `git` once the health check passes. It exits 0 without changes when any admin exists, and otherwise runs `gitea admin user create --admin` with the username, email, and password passed in the exec environment. `INSTALL_LOCK=true` from the first start means the install page is never served. See [One-Click Services](../guides/services.md#gitea).

---

### App templates: ntfy, Umami, SearXNG, Listmonk, Stirling-PDF, PocketBase, authentik, Meilisearch

All eight are `web: true` single-container templates: the first port is the one Traefik routes to, and no port is published on the host. Each has a health check and a `connection.internal` of `http://<container>:<port>`. See [One-Click Services](../guides/services.md#apps-that-need-postgresql-umami-listmonk-authentik) for usage.

| File | Category | Image (default version) | Port | Volumes | Config fields |
|------|----------|-------------------------|------|---------|---------------|
| `templates/ntfy.yaml` | `monitoring` | `binwiederhier/ntfy:v2.35.0` | 80 | `data` → `/var/lib/ntfy` | `default_access` (select, `deny-all`), `enable_signup` (bool), `admin_username`, `admin_password` (generated, immutable) |
| `templates/umami.yaml` | `monitoring` | `ghcr.io/umami-software/umami:3.4.1` | 3000 | none | `database_url` (password, required), `app_secret` (generated, immutable), `two_factor_key` (optional) |
| `templates/searxng.yaml` | `productivity` | `searxng/searxng:latest` | 8080 | `config` → `/etc/searxng`, `cache` → `/var/cache/searxng` | `secret_key` (generated, immutable) |
| `templates/listmonk.yaml` | `productivity` | `listmonk/listmonk:v6.2.0` | 9000 | `uploads` → `/listmonk/uploads` | `db_host`, `db_port`, `db_name`, `db_user`, `db_password`, `db_ssl_mode` (select), `admin_username` (immutable), `admin_password` (generated, immutable) |
| `templates/stirlingpdf.yaml` | `productivity` | `stirlingtools/stirling-pdf:3.2.2` | 8080 | `configs` → `/configs`, `tessdata` → `/usr/share/tessdata` | `enable_login` (bool, `false`), `default_locale`, `max_file_size_mb` |
| `templates/pocketbase.yaml` | `devtools` | `ghcr.io/muchobien/pocketbase:0.40.6` | 8090 | `data` → `/pb_data` | `admin_email` (optional), `admin_password` (generated, immutable) |
| `templates/authentik.yaml` | `security` | `ghcr.io/goauthentik/server:2026.8.5` | 9000 | `data` → `/data` | `mode` (select `allinone`/`server`/`worker`, rendered as the command), `db_host`, `db_port`, `db_name`, `db_user`, `db_password`, `secret_key` (generated, immutable), `bootstrap_email`, `bootstrap_password` (generated, immutable) |
| `templates/meilisearch.yaml` | `database` | `getmeili/meilisearch:v1.56.0` | 7700 | `data` → `/meili_data` | `master_key` (generated, 32 chars, immutable) |

Every template except SearXNG also offers `latest`. Umami, Listmonk, and authentik need a PostgreSQL database that GoForge does not create; Listmonk and authentik take separate host, port, name, user, and password fields because neither accepts a connection string.

- **ntfy** `post_start` `create-admin`: `ntfy user add --role=admin` with the password in `NTFY_PASSWORD` (not argv); exits 0 when the user exists.
- **Listmonk** `command`: upstream's `./listmonk --install --idempotent --yes --config '' && ./listmonk --upgrade --yes --config '' && exec ./listmonk --config ''`. `LISTMONK_ADMIN_USER` / `LISTMONK_ADMIN_PASSWORD` apply only while installing on an empty database.
- **PocketBase**: `PB_ADMIN_EMAIL` and `PB_ADMIN_PASSWORD` are `optional_environment` and both render empty when `admin_email` is blank, so the image's entrypoint skips its `superuser upsert`.
- **authentik**: `AUTHENTIK_BOOTSTRAP_*` are `optional_environment`; authentik applies them only when it creates `akadmin`. Health check `ak healthcheck` works in every mode.

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
- **password fields**: Set `generate: true` to auto-generate secure passwords. Fields of type `password` are masked on the service page behind **Show** and **Copy**, and so is any connection string that contains one
- **argon2id**: `{{ argon2id .Config.field }}` renders an Argon2id PHC hash (OWASP preset: 19 MiB, 2 passes, 1 lane) with a fresh salt for services that take a hashed secret. An empty value stays empty
- **health_check**: Always include a health check for reliable startup detection
- **volumes**: Always use named volumes for data persistence
- **resources**: Set reasonable defaults; users cannot currently override these through the UI
