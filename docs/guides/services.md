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
| Allow Signups | `true` | `SIGNUPS_ALLOWED` at first start. Turn it off in `/admin` after you create your account |
| Admin Panel Token | Generated on the server when the field is left blank | Sign-in token for `/admin`, stored encrypted and masked on the service page. The container gets an Argon2id hash as `ADMIN_TOKEN` |
| Storage | Docker volume | `/data` (SQLite vault, attachments, keys) |

Bitwarden clients and the web vault need HTTPS, so give Vaultwarden a public HTTPS domain.

To close signups after the first account, open `/admin`, sign in with the admin token, turn off **Allow new signups** under General settings, and save. Settings saved in `/admin` are written to `/data/config.json` and override the environment.

!!! warning "Do not redeploy to change settings or upgrade"
    A new deploy is a new instance with a new, empty `/data` volume, so deleting and redeploying Vaultwarden loses the vault. Use **Edit configuration** or **Update image** on the service page instead: both recreate the container on the same volume. Settings saved in `/admin` also stay in `/data`.

### Gitea

A self-hosted Git service.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 1.25.5 | `gitea/gitea` tag. `latest` is also offered |
| Port | 3000 | HTTP port Traefik routes to. Port 22 (SSH) is declared but not reachable, see below |
| Domain | `gitea.localhost` | Public host, for example `gitea-app.example.com`. Sets `DOMAIN` and `ROOT_URL` |
| Application Name | `Gitea: Git with a cup of tea` | `APP_NAME` shown in the page title |
| Disable Registration | `true` | `[service] DISABLE_REGISTRATION`. Only admins can create accounts |
| Require Sign-in to View | `true` | `[service] REQUIRE_SIGNIN_VIEW`. Anonymous visitors see only the sign-in page |
| Admin Username | `gitea-admin` | First admin account, created by GoForge. `admin` is reserved by Gitea |
| Admin Email | blank | Email of the first admin. Blank uses `admin@<Domain>`, or `admin@<container name>` when no Domain is set |
| Admin Password | Generated (32 chars) when left blank | Password of the first admin, stored encrypted and masked on the service page behind **Show** and **Copy** |
| Secret Key | Generated (64 chars) when left blank | `[security] SECRET_KEY`, which Gitea uses to encrypt stored secrets. Set once at deploy |
| Storage | Docker volume | `/data` (SQLite database, repositories, `app.ini`) |

Gitea always listens on plain HTTP (`PROTOCOL=http`). Traefik, and Cloudflare in proxy mode, end TLS in front of it. Only `ROOT_URL` carries the browser URL: `https://<domain>/` for a public host, `http://<domain>/` for a `.localhost` host. Do not set Gitea's `PROTOCOL` to `https`. Gitea then expects TLS on port 3000 and the Traefik route returns errors.

When you enter a Domain, GoForge adds the Traefik labels itself: a ``Host(`<domain>`)`` router on the `web` entrypoint in Cloudflare proxy mode or for a `.localhost` host (otherwise `websecure` with a Let's Encrypt certificate and an HTTP to HTTPS redirect) and a load balancer on port 3000, the first port in the template. For a public host, create the proxied Cloudflare CNAME first (see [Public HTTPS behind Cloudflare Tunnel](#public-https-behind-cloudflare-tunnel)).

**First start.** There is no install page. GoForge starts Gitea with `[security] INSTALL_LOCK=true`, so the installer is never served, not even for the seconds before the first account exists. Gitea uses SQLite in `/data`. Once the container's health check passes, GoForge runs the template's `post_start` hook inside the container as user `git`. The hook creates the admin account from **Admin Username**, **Admin Email**, and **Admin Password** with `gitea admin user create --admin`, and only when Gitea has no admin yet. The deploy turns `running` after the hook succeeds. Sign in at your Domain with the admin username and the password from the service page (**Show** or **Copy**). If the container never becomes healthy or the hook exits non-zero, the instance turns `failed` and the GoForge log has the hook name, exit code, and duration. The container and its `/data` volume are left in place.

The hook gets the password through exec environment variables, so it is not in the container's environment, the hook command GoForge sends, or the GoForge log. Gitea's CLI only takes the password as a `--password` argument, though, so for the fraction of a second `gitea admin user create` runs, the password is in that process's command line. Any local user on the GoForge host can read it from the host's `/proc` unless `/proc` is mounted with `hidepid=2`. If other people have shell access to the host, mount `/proc` with `hidepid=2` or change the admin password in Gitea after the first sign-in.

**Admin account after the first start.** GoForge creates the admin once and never changes it. Change the password, email, or name in Gitea (**Site Administration** or your user settings). The value on the service page is the password GoForge created the account with. **Admin Password** and **Secret Key** are set once at deploy, so **Edit configuration** does not show them. Editing the other admin fields later has no effect once an admin exists.

**Secret key.** Gitea encrypts stored secrets, such as two-factor keys and mirror credentials, with `SECRET_KEY`. GoForge generates one at deploy and passes it as `GITEA__security__SECRET_KEY` on every start, so it stays the same across **Edit configuration** and **Update image**. It must never change, which is why it cannot be edited. A Gitea instance deployed before this field existed has no stored key. GoForge then leaves the variable out, and Gitea keeps the `SECRET_KEY` already in its `app.ini`. The admin hook still runs on every recreate of such an instance and does nothing because an admin exists.

**SSH.** GoForge does not publish container ports on the host, and Traefik routes only HTTP. Port 22 is not reachable from outside the Docker network, so clone and push over HTTPS with a personal access token. SSH clone URLs that Gitea shows do not work.

!!! warning "Do not redeploy to change settings or upgrade"
    As with Vaultwarden, a new deploy is a new instance with a new, empty `/data` volume, so repositories and users are lost. Use **Edit configuration** or **Update image** on the service page instead: both recreate the container on the same volume and keep the secret key. Settings that come from the environment (domain, URLs, registration, install lock, secret key) are rewritten into `app.ini` on every container start, so change those in GoForge. Change everything else in **Site Administration**.

### Public HTTPS behind Cloudflare Tunnel

With `CLOUDFLARE_PROXY_MODE=true`, Cloudflare ends TLS and forwards HTTP to Traefik. GoForge routes the service on the `web` entrypoint, and templates still get `Protocol` = `https` for any domain that is not `localhost` or `*.localhost`, because that is the URL a browser uses.

Use a first-level host such as `vaultwarden-app.example.com` or `gitea-app.example.com`. Cloudflare Universal SSL covers `*.example.com` but not `*.apps.example.com`. Create a proxied CNAME for the host that points at your tunnel (`<tunnel-id>.cfargotunnel.com`) before you deploy, then enter the host in the **Domain** field.

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
| **Edit configuration** | Change name, domain, or template settings and recreate the container on the same volumes |
| **Update image** | Pull the service's image again and recreate the container with the same config and volumes |
| **Restart** | Restart the service container |
| **Delete** | Stop and remove the service and its data |

### Image updates

GoForge pulls the image every time it deploys a service or recreates its container, unless the image is pinned by digest (`name@sha256:...`). A tag such as `latest` can point at a newer image upstream, and the pull picks it up. A pull of an unchanged tag only checks the registry and downloads nothing. If the registry can't be reached during a deploy or a configuration edit, GoForge logs a warning and uses the local copy of the image.

**Update image** pulls first and only then stops the old container, so the service stays up while the image downloads. If the pull fails, the running container is left as it was and the error is in the GoForge logs. The new container keeps the instance ID, so it mounts the same `goforge-vol-<instance-id>-<volume>` volumes.

For predictable upgrades, prefer a pinned version tag (for example `gitea/gitea:1.25.5`) or a digest over `latest`. With a pinned tag, an upgrade is a template change to a new version, not a surprise on the next deploy.

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

1. Create a YAML file (`*.yaml`) in your `TEMPLATES_DIR` (default `./templates`)
2. Follow the template schema shown above, with `config` for user inputs and `docker` for container settings
3. Open **Services** (`/services`). The new template appears there.

### How the catalog loads and reloads

- At startup GoForge reads every `*.yaml` file in `TEMPLATES_DIR`. It logs one line with the template count and the absolute directory, for example `msg="loaded service templates" count=5 dir=/app/templates`.
- The service catalog page and each **Configure** page check the directory again before they render. GoForge reloads the catalog when a `*.yaml` file was added, removed, or changed (by name, size, or modification time), and logs `service catalog reloaded` with the new count. You don't need a restart or an image rebuild.
- A file that fails to parse is skipped and logged as a warning. Every other template keeps working. The broken template returns once its file is fixed.
- A reload swaps the whole in-memory catalog at once, so a request never sees a half-loaded catalog. Services that are already deployed are not affected.

### Custom templates with Docker Compose

The image includes the repository's `templates/` directory at `/app/templates` (`TEMPLATES_DIR=./templates`, relative to `/app`). A new file inside the image only appears after a rebuild. Mount a directory from the host instead, so you can edit templates without rebuilding:

```yaml
# docker-compose.override.yml
services:
  goforge:
    volumes:
      - ./templates:/app/templates:ro
```

Recreate the `goforge` container once, for example with `docker compose up -d goforge`, so the mount takes effect. After that, files you add or edit under `./templates` on the host appear the next time you open `/services`.

The mount replaces the bundled templates, so mount a directory that holds a copy of them (the repo's `templates/` checkout does). To keep your templates separate, mount them elsewhere and point `TEMPLATES_DIR` there, for example `- ./my-templates:/app/custom-templates:ro` with `TEMPLATES_DIR=/app/custom-templates` in `.env`.

See [Service Templates Reference](../reference/service-templates.md) for the complete template specification.
