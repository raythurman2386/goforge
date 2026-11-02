# One-Click Services

GoForge provides a catalog of pre-configured services that can be deployed with a single click.

!!! note "Docker Required"
    Service deployment requires GoForge to have access to the Docker daemon (via Docker socket). This is available when running via `docker-compose`, not when running locally with `make dev`.

## Available Services

### PostgreSQL

A relational database management system.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 17 | PostgreSQL major version. Changing major versions later needs a dump and restore |
| Port | 5432 | Container port |
| Database | `app` | Database created at first start. Set at deploy, cannot be edited |
| Username | `pgadmin` | Admin (superuser) role. Set at deploy, cannot be edited |
| Password | Generated on the server (32 chars) when the field is left blank | Admin password, stored encrypted. Set at deploy, cannot be edited |
| Resources | 1 CPU, 1GiB | Container limits |
| Storage | Docker volume | Persistent data storage |

### Redis

An in-memory data store for caching, sessions, and message queues.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 7.4 | Redis version |
| Port | 6379 | Container port |
| Password | Generated on the server when the field is left blank | Password of the `default` user, required for every client, stored encrypted. Written to `/data/users.acl` as a SHA-256 hash on every start (no `requirepass`). Never in the container command or health check argv |
| Persistence | AOF (`appendonly yes`, `everysec`) plus RDB snapshots | Files in the `data` volume (`/data`) |
| Memory | `maxmemory 256mb`, `noeviction`; container limit 512MiB | A full instance refuses writes with `OOM` instead of evicting keys (sessions and queues) |

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

After that, `/admin` is the place to check or change signups. The **Allow Signups** field on the GoForge service page only sets `SIGNUPS_ALLOWED`, which `/data/config.json` overrides, so it no longer shows the live setting. The public `GET /api/config` field `disableUserRegistration` does not tell you either: Vaultwarden reports `true` only when invitations are also off or SMTP is configured.

!!! warning "Do not redeploy to change settings or upgrade"
    A new deploy is a new instance with a new, empty `/data` volume, so deleting and redeploying Vaultwarden loses the vault. Use **Edit configuration** or **Update image** on the service page instead: both recreate the container on the same volume. Settings saved in `/admin` also stay in `/data`.

### Gitea

A self-hosted Git service.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 1.25.5 | `gitea/gitea` tag. `latest` is also offered |
| Port | 3000 | HTTP port Traefik routes to. Port 22 (SSH) is published on `127.0.0.1` only, see below |
| Domain | `gitea.localhost` | Public host, for example `gitea-app.example.com`. Sets `DOMAIN` and `ROOT_URL` |
| Application Name | `Gitea: Git with a cup of tea` | `APP_NAME` shown in the page title |
| Disable Registration | `true` | `[service] DISABLE_REGISTRATION`. Only admins can create accounts |
| Require Sign-in to View | `true` | `[service] REQUIRE_SIGNIN_VIEW`. Anonymous visitors see only the sign-in page |
| Admin Username | `gitea-admin` | First admin account, created by GoForge. `admin` is reserved by Gitea |
| Admin Email | blank | Email of the first admin. Blank uses `admin@<Domain>`, or `admin@<container name>` when no Domain is set |
| Admin Password | Generated (32 chars) when left blank | Password of the first admin, stored encrypted and masked on the service page behind **Show** and **Copy** |
| Secret Key | Generated (64 chars) when left blank | `[security] SECRET_KEY`, which Gitea uses to encrypt stored secrets. Set once at deploy |
| SSH Port | `2223` | Host port for Gitea SSH, bound on `127.0.0.1` only. Also `[server] SSH_PORT`, the port in SSH clone URLs |
| SSH Domain | `minis` | `[server] SSH_DOMAIN`, the host in SSH clone URLs, for example a Tailscale MagicDNS name. Blank uses the Domain |
| Storage | Docker volume | `/data` (SQLite database, repositories, `app.ini`) |

Gitea always listens on plain HTTP (`PROTOCOL=http`). Traefik, and Cloudflare in proxy mode, end TLS in front of it. Only `ROOT_URL` carries the browser URL: `https://<domain>/` for a public host, `http://<domain>/` for a `.localhost` host. Do not set Gitea's `PROTOCOL` to `https`. Gitea then expects TLS on port 3000 and the Traefik route returns errors.

When you enter a Domain, GoForge adds the Traefik labels itself: a ``Host(`<domain>`)`` router on the `web` entrypoint in Cloudflare proxy mode or for a `.localhost` host (otherwise `websecure` with a Let's Encrypt certificate and an HTTP to HTTPS redirect) and a load balancer on port 3000, the first port in the template. For a public host, create the proxied Cloudflare CNAME first (see [Public HTTPS behind Cloudflare Tunnel](#public-https-behind-cloudflare-tunnel)).

**First start.** There is no install page. GoForge starts Gitea with `[security] INSTALL_LOCK=true`, so the installer is never served, not even for the seconds before the first account exists. Gitea uses SQLite in `/data`. Once the container's health check passes, GoForge runs the template's `post_start` hook inside the container as user `git`. The hook creates the admin account from **Admin Username**, **Admin Email**, and **Admin Password** with `gitea admin user create --admin`, and only when Gitea has no admin yet. The deploy turns `running` after the hook succeeds. Sign in at your Domain with the admin username and the password from the service page (**Show** or **Copy**). If the container never becomes healthy or the hook exits non-zero, the instance turns `failed` and the GoForge log has the hook name, exit code, and duration. The container and its `/data` volume are left in place.

The hook gets the password through exec environment variables, so it is not in the container's environment, the hook command GoForge sends, or the GoForge log. Gitea's CLI only takes the password as a `--password` argument, though, so for the fraction of a second `gitea admin user create` runs, the password is in that process's command line. Any local user on the GoForge host can read it from the host's `/proc` unless `/proc` is mounted with `hidepid=2`. If other people have shell access to the host, mount `/proc` with `hidepid=2` or change the admin password in Gitea after the first sign-in.

**Admin account after the first start.** GoForge creates the admin once and never changes it. Change the password, email, or name in Gitea (**Site Administration** or your user settings). The value on the service page is the password GoForge created the account with. **Admin Password** and **Secret Key** are set once at deploy, so **Edit configuration** does not show them. Editing the other admin fields later has no effect once an admin exists.

**Secret key.** Gitea encrypts stored secrets, such as two-factor keys and mirror credentials, with `SECRET_KEY`. GoForge generates one at deploy and passes it as `GITEA__security__SECRET_KEY` on every start, so it stays the same across **Edit configuration** and **Update image**. It must never change, which is why it cannot be edited. A Gitea instance deployed before this field existed has no stored key. GoForge then leaves the variable out, and Gitea keeps the `SECRET_KEY` already in its `app.ini`. The admin hook still runs on every recreate of such an instance and does nothing because an admin exists.

**SSH.** Traefik routes only HTTP, so GoForge publishes Gitea's SSH port (container port 22) on the host at `127.0.0.1:<SSH Port>`, `2223` by default. It binds loopback only: the port is not reachable from the LAN, the tailnet, or the internet, and GoForge refuses to publish a template port on `0.0.0.0` or `::`. Reach it from another machine by jumping through the host's own SSH server, which already accepts your key:

```
# ~/.ssh/config on your laptop
Host gitea-ssh
    HostName 127.0.0.1
    Port 2223
    User git
    ProxyJump you@minis
```

Then `git clone gitea-ssh:owner/repo.git`, or `ssh -J you@minis -p 2223 git@127.0.0.1` to test. Add your public key in Gitea under **Settings → SSH / GPG Keys** first. Gitea's clone URLs show `ssh://git@<SSH Domain>:<SSH Port>/owner/repo.git`; with **SSH Domain** set to the host's tailnet name and a matching `Host` entry (or `ProxyJump`), they work as shown. HTTPS with a personal access token keeps working as before.

Pick an **SSH Port** that nothing else on the host uses: not `22` (the host's sshd) and not a port a rollback or other container publishes. Changing **SSH Port** with **Edit configuration**, or the first **Edit configuration** or **Update image** of a Gitea instance deployed before this setting existed, recreates the container and binds the port. If the port is taken, the new container cannot start: GoForge puts the previous container back, starts it again with its previous settings, and marks the instance `failed` so you can pick another port. Nothing is removed.

!!! warning "Do not redeploy to change settings or upgrade"
    As with Vaultwarden, a new deploy is a new instance with a new, empty `/data` volume, so repositories and users are lost. Use **Edit configuration** or **Update image** on the service page instead: both recreate the container on the same volume and keep the secret key. Settings that come from the environment (domain, URLs, registration, install lock, secret key) are rewritten into `app.ini` on every container start, so change those in GoForge. Change everything else in **Site Administration**.

### Gitea Actions Runner

A [Gitea Actions](https://docs.gitea.com/usage/actions/overview) runner from the official `gitea/act_runner` image. Jobs run in act_runner host mode inside this one container, which has Python, Node.js, TypeScript, Go, and Rust installed.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | 0.6.1 | `gitea/act_runner` tag (the basic Alpine image, not `-dind`). `latest` is also offered |
| Gitea URL | none | Gitea's `ROOT_URL`, for example `https://gitea-app.example.com`. The runner polls it, and `actions/checkout` clones from it |
| Runner Registration Token | none | From Gitea, used once to register. Stored encrypted. Not needed after registration |
| Runner Name | `goforge-runner` | Name shown in Gitea's runner list. Applied at registration |
| Labels | `linux,alpine,python,node,typescript,go,rust` | Comma-separated `runs-on` labels. Each becomes `<name>:host` |
| Concurrent Jobs | 1 | act_runner `capacity` |
| Extra Alpine Packages | none | Space-separated `apk` packages added on every container create, for example `uv postgresql-client` |
| Global npm Packages | `typescript` | Installed with `npm install --global` on every container create |
| Resources | 2 CPU, 4GiB | Raise them for large Rust or Go builds |
| Storage | Docker volumes | `data` at `/data` (`.runner` registration, generated `config.yaml`) and `cache` at `/cache` (apk packages, Go modules and build cache, Cargo registry, npm and pip caches, `actions/cache` data, job workdirs) |

**Register.** In Gitea, open **Site Administration → Actions → Runners** for an instance-wide runner (or a repository's or organization's **Settings → Actions → Runners**), click **Create new Runner**, and copy the registration token. Deploy the runner with the Gitea URL and that token. On first start the image's `run.sh` registers the runner, stores the result in `/data/.runner`, and unsets the token before `act_runner daemon` starts. The runner then shows as online in that Runners list. Later creates (**Edit configuration**, **Update image**) reuse `/data/.runner` and need no token. If you change **Gitea URL**, add a new token from that Gitea as well: the old registration moves to `/data/.runner.previous` and the runner registers again. Without a token the container stops with a message in its logs and changes nothing.

**Toolchains and labels.** On every container create, the entrypoint installs these Alpine packages before the runner starts: `python3`, `py3-pip`, `nodejs`, `npm`, `go`, `rust`, `cargo`, `rust-clippy`, `rustfmt`, `build-base`, `git`, `git-lfs`, `openssh-client`, `curl`, `jq`, and archive tools. It also installs the global npm packages, which is `tsc` by default. The packages come from the image's Alpine release (3.23 for 0.6.1: Python 3.12, Node 24, Go 1.25, Rust 1.91). The apk cache on the `cache` volume makes a recreate quick: a warm recreate was ready in about 8 seconds in testing. A cold first start downloads about 350MB. Every label maps to the same container, so a label names a toolchain for the workflow reader and does not pick a different environment. Pick one in a workflow:

```yaml
jobs:
  test:
    runs-on: go          # or python, node, typescript, rust, linux, alpine
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...
```

The entrypoint writes `/data/config.yaml` from the fields on every create, and act_runner declares the labels from it at startup. A label edit therefore takes effect on the next create without registering again.

**Limits.** GoForge never mounts the Docker socket into service containers (see "Docker socket trust model" in `SECURITY.md`) and has no privileged option. Jobs therefore cannot run `docker build`, `docker://` labels, `container:`, or `services:`. The entrypoint refuses any label that is not a host label. Jobs run as root in the runner container and share its filesystem and caches, so register it only for repositories you trust. `actions/setup-node`, `setup-python`, and similar actions download glibc builds that do not run on Alpine (musl). Use the preinstalled toolchains instead. `actions/setup-go` works because Go binaries are static. Python packages install into a virtualenv (`python3 -m venv .venv`), because Alpine marks the system Python as externally managed. `actions/cache` works through act_runner's built-in cache server, with its data on the `cache` volume.

### Apps that need PostgreSQL (Umami, Listmonk, authentik)

These templates run one container and keep their data in PostgreSQL. GoForge does not create the database for them. Before you deploy one:

1. Deploy the **PostgreSQL** service, or use the shared one (see [Shared data services](#shared-data-services)).
2. Create the app's role and database: `scripts/shared-data/pg-tenant.sh <postgres-container> <app>`.
3. Enter that container name, database, user, and password in the app's configure form: a `postgresql://` connection string for Umami, separate host, port, name, user, and password fields for Listmonk and authentik (neither takes a connection string).

| Service | Image | Port | Volume | Notes |
|---------|-------|------|--------|-------|
| Umami | `ghcr.io/umami-software/umami:3.4.1` | 3000 | none (stateless) | Creates its tables on first start. First login `admin` / `umami`: change it at once. Two-factor needs a 64-hex-char key (`openssl rand -hex 32`) |
| Listmonk | `listmonk/listmonk:v6.2.0` | 9000 | `uploads` → `/listmonk/uploads` | Runs `--install --idempotent` then `--upgrade` on every start, so the schema is created once and migrated after **Update image**. The admin (`listmonk-admin`, generated password) is created only while installing on an empty database. Set Root URL and SMTP under **Settings** |
| authentik | `ghcr.io/goauthentik/server:2026.8.5` | 9000 | `data` → `/data` | No Redis (removed in 2025.10). **Mode** `allinone` (default) runs server and worker in one container; `server` and `worker` split them across two deploys that share the database and **Secret Key**. Sign in as `akadmin` with the generated bootstrap password |

authentik limits in a single GoForge template: the worker has no Docker socket, so outposts are deployed by hand; Docker's `shm_size` cannot be set, so `/dev/shm` (authentik's `TMPDIR`) stays at Docker's 64 MB default instead of upstream's 512 MB; and a split server/worker pair has two separate `/data` volumes, so use `allinone` unless you need to scale.

### ntfy

Push notifications over HTTP (`binwiederhier/ntfy:v2.35.0`, port 80, `data` volume at `/var/lib/ntfy` for the message cache, users, and attachments). `NTFY_BASE_URL` is the Domain URL and `NTFY_BEHIND_PROXY=true`. **Default Access** starts at `deny-all`, so anonymous clients can neither publish nor subscribe. A `post_start` hook creates the admin (`ntfy-admin`, generated password) with `ntfy user add --role=admin`, reading the password from `NTFY_PASSWORD` (never argv), and does nothing when that user exists. Give it a public HTTPS domain for the phone apps and web push.

### SearXNG

Metasearch engine (`searxng/searxng:latest`, port 8080). Volumes: `config` → `/etc/searxng` (`settings.yml`, created from the image template on first start) and `cache` → `/var/cache/searxng`. `SEARXNG_SECRET` (generated) and `SEARXNG_BASE_URL` (the Domain) come from the template. It runs without the optional Valkey limiter, so keep it private or behind Cloudflare Access.

### Stirling-PDF

PDF toolkit (`stirlingtools/stirling-pdf:3.2.2`, port 8080, 1 CPU / 2 GiB). Volumes: `configs` → `/configs` and `tessdata` → `/usr/share/tessdata` (extra OCR languages). **Enable Login** is off by default, so anyone who reaches the Domain can use it; keep it private or turn login on.

### PocketBase

Backend in one binary with SQLite (`ghcr.io/muchobien/pocketbase:0.40.6`, community image; port 8090, `data` volume at `/pb_data`, dashboard at `/_/`). Enter **Superuser Email** to have the image create the superuser with the generated password; the image upserts it on every start, so that account's password returns to the stored value after a restart. Leave the email blank to create the superuser through the installer link in the container logs instead.

### Meilisearch

Search engine (`getmeili/meilisearch:v1.56.0`, port 7700, `data` volume at `/meili_data`). Runs with `MEILI_ENV=production` and a generated 32-char master key, set once at deploy. Clients use the master key or API keys created with it. A newer version may need a dump and import of `data.ms`, so pin the version and read the Meilisearch upgrade notes before **Update image**.

### GeoLibre

Browser GIS (MapLibre, DuckDB-WASM) from `ghcr.io/opengeos/geolibre:v3.6.0`, served by nginx on port 80. Data you open stays in the browser.

| Setting | Default | Description |
|---------|---------|-------------|
| Version | v3.6.0 | `ghcr.io/opengeos/geolibre` tag (`latest` tracks upstream main) |
| Python Processing Sidecar | disabled | `enabled` starts the bundled Python sidecar at `/sidecar` for file conversion and processing under `/data` |
| GeoLens Server URL | `off` | `off` hides GeoLens; otherwise an `https://` URL (or localhost), or the container refuses to start |
| Basic Auth User / Password | blank | Optional shared HTTP Basic Auth over the whole site; set both or neither |
| Storage | `data` → `/data` | Sidecar conversion root; unused while the sidecar is off |

- **HTTPS**: the app's Content Security Policy only lets it fetch remote data over HTTPS, so give it a public HTTPS domain (see below) or a `.localhost` domain. The shipped CSP also allows `http://localhost:*` in `connect-src`; that is fine for private use behind Traefik, but keep it in mind before exposing it publicly.
- **Share is off**: the template sets `GEOLIBRE_SHARE_URL=off`, which removes Share and the Project Gallery, so nothing is published to the hosted `share.geolibre.app`.
- **Sidecar**: off by default (smaller attack surface, less memory); `/sidecar/` answers 403 while it is off. If you turn it on, raise the memory limit for heavy conversions.
- **Basic Auth**: GoForge sets `GEOLIBRE_AUTH_USER` and `GEOLIBRE_AUTH_PASSWORD` only when both fields are filled. It is one shared login, not per-user accounts, and `/healthz` stays open. Like every template value, the password is in the container environment (visible to anyone who can run `docker inspect` on the host).
- Hosting under a subpath needs an image rebuild upstream; GoForge routes by host, so the app runs at the domain root.

### RustGIS

RustGIS web app (Rust compiled to WebAssembly, WebGPU or WebGL2) from `ghcr.io/opengeos/rustgis:0.5.0`, served by unprivileged nginx on port 8080. No configuration fields and no volume: files stay in the browser.

Offline start and install-as-app use a service worker, which needs HTTPS or `localhost`. Use a `.localhost` domain locally, or a public HTTPS domain (see below). ArcGIS sign-in from a self-hosted copy needs its own ArcGIS OAuth app.

### Public HTTPS behind Cloudflare Tunnel

With `CLOUDFLARE_PROXY_MODE=true`, Cloudflare ends TLS and forwards HTTP to Traefik. GoForge routes the service on the `web` entrypoint, and templates still get `Protocol` = `https` for any domain that is not `localhost` or `*.localhost`, because that is the URL a browser uses.

Use a first-level host such as `vaultwarden-app.example.com` or `gitea-app.example.com`. Cloudflare Universal SSL covers `*.example.com` but not `*.apps.example.com`. Create a proxied CNAME for the host that points at your tunnel (`<tunnel-id>.cfargotunnel.com`) before you deploy, then enter the host in the **Domain** field.

## Shared data services

One PostgreSQL and one Redis instance can serve several apps on the host ("shared-pg" and "shared-redis"). They are ordinary catalog instances:

1. Deploy **PostgreSQL** named `shared-pg` and **Redis** named `shared-redis` with the default versions (17 and 7.4).
2. Leave **Domain** empty and do not publish any port. They are reachable only from containers on the GoForge (`traefik`) network, as `goforge-service-<id>:5432` and `:6379`.
3. Check that Traefik does not route them:

    ```bash
    docker inspect --format '{{ index .Config.Labels "traefik.enable" }} {{ json .HostConfig.PortBindings }}' goforge-service-<id>
    # false {}
    ```

4. Give each app its own Postgres role and database, never the `pgadmin` account:

    ```bash
    printf '%s' "$APP_DB_PASSWORD" | scripts/shared-data/pg-tenant.sh goforge-service-<shared-pg id> <app>
    # app connects as postgres://<app>:<password>@goforge-service-<id>:5432/<app>
    ```

    The script is idempotent (run it again to rotate the password), gives the role `LOGIN` only, makes it the owner of its database, and revokes `CONNECT` from `PUBLIC` so other apps cannot open it.

5. Give each app its own Redis user, never the `default` user:

    ```bash
    printf '%s' "$APP_REDIS_PASSWORD" | scripts/shared-data/redis-tenant.sh goforge-service-<shared-redis id> <app>
    # app connects as redis://<app>:<password>@goforge-service-<id>:6379
    # and must prefix every key and channel with <app>:
    scripts/shared-data/redis-tenant.sh --revoke goforge-service-<shared-redis id> <app>
    ```

    The user can only touch keys and Pub/Sub channels matching `<app>:*`; anything else answers `NOPERM`, including inside Lua scripts. It gets `~<app>:* resetchannels &<app>:* +@all -@admin -@dangerous +info -scan -randomkey -pubsub`, plus `-function` on Redis 7 and later (6.2 has no `FUNCTION`). So `FLUSHALL`, `FLUSHDB`, `KEYS`, `CONFIG`, and `ACL` are denied; `SCAN`, `RANDOMKEY`, and `PUBSUB` (`CHANNELS`, `NUMSUB`, ...) are denied because they show other apps' key or channel names; and `FUNCTION` is denied because function libraries are shared by the whole instance (`FUNCTION FLUSH` would wipe every app's). Works on the template's 7.4, 7, and 6 (6.2) versions. The script is idempotent (run it again to rotate the password; connections already open stay logged in until they reconnect). `--revoke` deletes the user and leaves its keys. Changes are written to `/data/users.acl` with `ACL SAVE` and survive restarts. Point the app's client at a key prefix (for example ioredis `keyPrefix`, BullMQ `prefix`) of `<app>:`.

    The script needs an instance started by a template with the ACL file (#60). A Redis instance deployed before that gets it on its next recreate (**Edit configuration** or **Update image**). Until then the script checks `CONFIG GET aclfile` first, finds it empty, and exits 1 before creating, changing, or deleting any user, live or saved. If `ACL SAVE` fails on an instance that has the file, the script runs `ACL LOAD` so the live users match the saved file again, and exits 1.

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
| **Delete** | Stop and remove the service and its data. A dialog asks you to type the service name; the server refuses the delete (`400`) without the exact name |

### Protected services

The **Protected** switch on a service page keeps agents and API tokens away from it: through `/api/v1` and the MCP server, stopping its container, editing its configuration, or updating its image needs a token with the `protected` scope, and anything else answers `403 protected_resource` before any Docker call. Restart is still allowed with `write`. The switch changes nothing about the running container, and the web UI itself is not restricted.

The flag is stored on the service (migration `026_service_instances_protected`) and applies at once, with no restart. Each change is an audit row, `service.set_protected`, with `details.protected`, `details.previous`, and `via` = `ui`. A service listed in `GOFORGE_PROTECTED_SERVICES` shows the switch on and locked; remove it from that variable to turn protection off. GoForge protects a service when either the switch or the variable says so.

A service runs one **Edit configuration** or **Update image** at a time. While one is recreating the container, the service is `pending`, and another edit or update is refused with `409 Conflict`, even one sent at the same moment.

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
