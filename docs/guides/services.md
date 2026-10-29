# One-Click Services

GoForge provides a catalog of pre-configured services that can be deployed with a single click.

!!! note "Docker Required"
    Service deployment requires GoForge to have access to the Docker daemon (via Docker socket). This is available when running via `docker-compose`, not when running locally with `make dev`.

## Available Services

### PostgreSQL

A relational database management system.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 16 | PostgreSQL major version |
| Port | 5432 | Container port |
| Database | `app` | Default database name |
| Username | `postgres` | Admin username |
| Password | Generated on the server when the field is left blank | Admin password, stored encrypted |
| Storage | Docker volume | Persistent data storage |

### Redis

An in-memory data store for caching, sessions, and message queues.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 7 | Redis major version |
| Port | 6379 | Container port |
| Password | Optional | Auth password |
| Persistence | AOF + RDB | Data persistence mode |

### SolidCraft

Browser-based parametric CAD. The `solidcraft-web` image serves the static app with nginx.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | latest | `ghcr.io/tangency-labs/solidcraft-web` tag |
| Port | 8080 | nginx container port |
| Domain | `solidcraft.localhost` | Host Traefik routes to the service |
| Storage | None | Project files live in the browser's private storage |

The browser only grants private file storage on HTTPS or `localhost`. Use a `.localhost` domain locally, or a public HTTPS domain (for example a Cloudflare first-level host, see below).

### Vaultwarden

A Bitwarden-compatible password manager server.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | latest | `vaultwarden/server` tag |
| Port | 80 | Container port |
| Domain | `vaultwarden.localhost` | Public host. Sets `DOMAIN` to `<protocol>://<domain>` |
| Allow Signups | `true` | `SIGNUPS_ALLOWED`. Set to `false` after you create your account |
| Admin Panel Token | Generated on the server when the field is left blank | `ADMIN_TOKEN` for `/admin`, stored encrypted and shown on the service page |
| Storage | Docker volume | `/data` (SQLite vault, attachments, keys) |

Bitwarden clients and the web vault need HTTPS, so give Vaultwarden a public HTTPS domain.

To close signups after the first account, redeploy with **Allow Signups** set to `false`, or open `/admin` with the admin token and turn off **Allow new signups**. Settings saved in `/admin` are written to `/data/config.json` and override the environment.

### Public HTTPS behind Cloudflare Tunnel

With `CLOUDFLARE_PROXY_MODE=true`, Cloudflare ends TLS and forwards HTTP to Traefik. GoForge routes the service on the `web` entrypoint, and templates still get `Protocol` = `https` for any domain that is not `localhost` or `*.localhost`, because that is the URL a browser uses.

Use a first-level host such as `vaultwarden-app.example.com`. Cloudflare Universal SSL covers `*.example.com` but not `*.apps.example.com`. Create a proxied CNAME for the host that points at your tunnel (`<tunnel-id>.cfargotunnel.com`) before you deploy, then enter the host in the **Domain** field.

## Deploying a Service

1. Navigate to **Services** in the sidebar
2. Browse the service catalog
3. Click **Deploy** on the desired service
4. Configure any options (version, credentials, etc.)
5. Click **Confirm**

GoForge will:

1. Pull the Docker image
2. Create a container with the appropriate configuration
3. Set up persistent volumes for data
4. Configure health checks
5. Display connection information

## Managing Services

### Service Actions

| Action | Description |
|--------|-------------|
| **Restart** | Restart the service container |
| **Delete** | Stop and remove the service and its data |

### Connection Information

After deploying a service, GoForge displays connection details:

- **Host:** Container name or internal IP
- **Port:** Service port
- **Credentials:** Username and password
- **Connection string:** Ready-to-use connection URL. `config` and `connection_info` are encrypted at rest and decrypted when GoForge reads them

!!! tip "Connecting from deployed apps"
    Applications deployed through GoForge can connect to services using Docker's internal DNS. Use the container name as the hostname (e.g., `postgres-abc123:5432`).

## Service Templates

Services are defined by YAML templates in the `templates/` directory. Each template specifies:

```yaml
name: postgres
display_name: PostgreSQL
description: The world's most advanced open source relational database.
icon: postgres
category: database

versions:
  - "16"
  - "15"
  - "14"
default_version: "16"

config:
  - name: database
    label: Database Name
    type: text
    default: "app"
    required: true
  - name: username
    label: Username
    type: text
    default: "postgres"
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
  image: "postgres:{{ .Version }}"
  ports:
    - container: 5432
      protocol: tcp
  environment:
    POSTGRES_DB: "{{ .Config.database }}"
    POSTGRES_USER: "{{ .Config.username }}"
    POSTGRES_PASSWORD: "{{ .Config.password }}"
  volumes:
    - name: data
      path: /var/lib/postgresql/data
  health_check:
    test: ["CMD-SHELL", "pg_isready -U {{ .Config.username }}"]
    interval: 10s
    timeout: 5s
    retries: 5

connection:
  internal: "postgres://{{ .Config.username }}:{{ .Config.password }}@{{ .ContainerName }}:5432/{{ .Config.database }}"
```

Key differences from a flat structure:

- **`versions`** is a simple string array, with a separate `default_version` field
- **`config`** defines user-facing input fields (name, label, type, default, generate, required)
- **`docker`** nests all Docker-specific settings: `image`, `ports`, `environment`, `volumes`, `health_check`
- **`resources`** specifies CPU and memory limits
- **`connection`** provides a Go template for connection strings
- Template values use Go template syntax (e.g., `{{ .Version }}`, `{{ .Config.database }}`, `{{ .ContainerName }}`)

## Adding Custom Service Templates

To add a new service to the catalog:

1. Create a YAML file in the `templates/` directory (or your configured `TEMPLATES_DIR`)
2. Follow the template schema shown above, with `config` for user inputs and `docker` for container settings
3. Restart GoForge or reload templates

The template will appear in the service catalog automatically.

See [Service Templates Reference](../reference/service-templates.md) for the complete template specification.
