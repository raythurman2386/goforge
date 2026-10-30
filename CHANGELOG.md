# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Nightly backups to S3-compatible storage (R2, B2, MinIO) inside `goforge serve`, default `02:00 America/Chicago`, off while `BACKUP_S3_BUCKET` is empty (t1241u, t1263u). Each run uploads the GoForge database (`pg_dump -Fc`), every running Postgres service (`pg_dump -Fc` per database and `pg_dumpall --roles-only`, as the container's `POSTGRES_USER`), Gitea (SQLite online backup plus a tar of `/data`), and Vaultwarden (`/vaultwarden backup` plus a tar of `/data`), with a `manifest.json` of sizes and SHA-256 sums. Retention keeps the newest `BACKUP_RETENTION_DAILY` (7) complete days and prunes only after a successful run. `ENCRYPTION_KEY` is never uploaded. Backup only; restore is manual (`docs/deployment/backup.md`)
- `goforge backup run`: one backup now, for manual runs and smoke tests
- Migration `019_backup_runs`; Settings → Profile shows the last backup to admins; `GET /api/v1/backups` (read scope, admin) and MCP tool `get_backup_status`
- Runtime image installs `postgresql17-client`
- JSON API `/api/v1` for automation (t1250u): bearer token only, outside the CSRF group, session cookies ignored; JSON errors with stable codes; ownership answers match the UI (#28). Reads mask secret env vars and service config; there is no secret-reveal route. Mutations are dry runs unless `"dry_run": false`, a real run needs `approval_ref`, and an `api_v1.<action>` audit row (approval_ref, token id) is written before anything changes; a failed audit insert blocks the change
- API token scopes `read`, `write`, `protected` (migration `018_api_token_scopes`); chosen on Settings → Profile → API Tokens. Existing and unscoped tokens are `full`. Scoped tokens only work on `/api/v1` (t1250u)
- `GOFORGE_PROTECTED_SERVICES`: service instance IDs whose stop, config edit, and image update through `/api/v1` need the `protected` scope (`403 protected_resource` before any Docker call); restart stays allowed with `write` (t1250u)
- `cmd/goforge-mcp`: MCP server over stdio (go-sdk v1.8.0) with 23 read tools (including `get_backup_status`) and, with `GOFORGE_MCP_ALLOW_WRITES=true`, 14 gated write tools; calls `/api/v1` with a scoped token, holds no Docker socket or database. `make build-mcp`, `make docker-build-mcp`, Dockerfile target `mcp`. Docs: `docs/reference/api-v1.md`, `docs/guides/mcp.md` (SSH + stdio) (t1250u)
- One-click SolidCraft (`solidcraft`) and Vaultwarden (`vaultwarden`) service templates
- Gitea template fields for registration (`disable_registration`) and sign-in to view (`require_signin_view`), both `true` by default, and Gitea docs for public `-app` hosts behind Cloudflare Tunnel
- First-Time User Experience (FTUX) onboarding flow with a step-by-step dashboard guide
- Clear empty state warnings when attempting to create a project without a Git Source connected
- Initial project setup
- GitHub CI workflow with fmt, vet, lint, and test checks
- Project documentation (README, CONTRIBUTING, LICENSE)
- `GetFileContent` implementation for Gitea and GitLab git providers
- Service restart handler (`POST /services/{id}/restart`)
- Real-time container resource monitoring (stats) to standard Git Projects
- Service template `post_start` hooks: commands GoForge runs with `docker exec` once a new or recreated container is healthy. A failed hook, or one still running at the 10-minute deadline, marks the instance `failed`; hook env is never logged (#43)
- Service template `immutable` config fields (set once at deploy, never changed by an edit) and `docker.optional_environment` (left out when empty) (#43)
- Gitea template creates the first admin (`admin_username`, default `gitea-admin`; `admin_email`, default `admin@<Domain>`; generated 32-char `admin_password`, masked on the service page) and generates a 64-char `SECRET_KEY` at deploy (#43)
- Service template config field types `bool` (checkbox, always `true`/`false`) and `select` (drop-down of `options`), validated on deploy and on config edit (#44)
- Service template ports can be published on the host with `publish`, `host_port` (Go template), and `host_ip` (default `127.0.0.1`, loopback only; `0.0.0.0` and `::` are refused). Published ports are recorded on the container and shown on the service page (#42)
- Gitea publishes SSH (container port 22) on `127.0.0.1:<ssh_port>`; new fields `ssh_port` (default `2223`) and `ssh_domain` (default `minis`) set `[server] SSH_PORT` and `SSH_DOMAIN` so clone URLs match (#42)
- Service template `docker.command`: a list that replaces the image CMD, rendered like `environment`. Templates must not render secrets into it (#58)
- `scripts/shared-data/pg-tenant.sh`: idempotently creates or updates one app's `LOGIN` role and owned database on a GoForge postgres instance, password on stdin, `CONNECT` revoked from `PUBLIC`; fails closed without changing anything if a database of that name already exists under another owner. Not run by GoForge (#59)
- Docs: shared data services (shared-pg / shared-redis with no Domain and no published port) and a shared-pg nightly `pg_dump` runbook (not installed) (#59)

### Changed
- Docs: `CONTRIBUTING.md` rewritten as the one contributor guide (house rules tied to checks, test types, migrations, service templates, PR format); `docs/development/index.md` points to it; `GEMINI.md` points to `AGENTS.md`; PR template gains Tests, Proof, and Deferred sections; README, schema reference (migrations 016 to 019), project structure, Compose backup sections, and testing docs brought up to date with #61 and #62 (t1275u)
- Router: global middleware is recovery and rate limit only; session and API-token auth wrap the UI routes, and `/api/v1` mounts beside them. `docs/reference/api-routes.md` brought in line with the routes (t1250u)
- Dependencies: `golang.org/x/crypto`, `x/oauth2`, `x/sync`, `x/time`, `x/tools` minor bumps pulled in by `github.com/modelcontextprotocol/go-sdk` v1.8.0 (t1250u)
- PostgreSQL template defaults to 17 (16, 15, 14 still offered), admin username `pgadmin` instead of `postgres`, a 32-char generated password, and 1 CPU / 1GiB; `database`, `username`, and `password` are immutable because the image applies them only on first init (#59)
- Redis template defaults to 7.4 (7 and 6 still offered) and is set up for sessions and queues: `appendonly yes`, `appendfsync everysec`, `maxmemory 256mb`, `maxmemory-policy noeviction`, written into the per-start conf file next to `requirepass`; container memory 512MiB (#59)
- Recreating a service container that publishes a host port (config edit, image update) renames the old container aside and removes it only after the new one starts; if the new one cannot start, the old one is restored and started again (#42)
- A config field missing from an older instance's stored config renders its template `default` instead of an empty string (#42)
- `docker.ContainerOptions.PortMappings` is a list of bindings with an explicit host IP; an empty or unspecified (`0.0.0.0`, `::`) host IP is refused (#42)
- Vaultwarden `signups_allowed` and Gitea `disable_registration` / `require_signin_view` are `bool` checkboxes instead of free text, so a typo can no longer reach the container environment (#44)
- Gitea template pins `gitea/gitea:1.25.5` by default (`latest` still offered), sets `PROTOCOL=http` and `DOMAIN` explicitly so Gitea listens on HTTP behind Traefik while `ROOT_URL` is the `https://` browser URL
- Service templates get `Protocol` = `https` for public domains in Cloudflare proxy mode (Cloudflare ends TLS), so `DOMAIN`/`ROOT_URL` style settings match the browser URL
- Renamed project to GoForge
- Simplified `doRequest` in Gitea and GitLab clients (removed redundant URL construction)
- Excluded gosec G115 (integer overflow) globally in `.golangci.yml` instead of per-line nolint directives
- Updated README.md to remove false claims of SQLite support
- Added UI toast feedback for "Prune System" action in settings
- Added `/goforge` build artifact to `.gitignore`

### Deprecated

### Removed
- Removed orphaned `CustomDomain` model and `internal/domain` logic that had no database repository

### Fixed
- `TestInstanceSource` flake in `internal/backup`: fake exec matching is deterministic (#63)
- Redis template did not enforce its password: the official image ignores `REDIS_PASSWORD`, so any client on the network could connect without `AUTH`. The template now starts `redis-server` with `requirepass` from a config file rewritten on every start (so restarts work), keeps the password out of the command line, and authenticates its health check. Existing Redis instances get auth on their next recreate (config edit or image update) (#58)
- One-click services without a Domain (Postgres, Redis, any instance with no Domain) were labelled `traefik.enable=true`, so Traefik built a default `Host(<container name>)` router to them on the web entrypoint. They now get `traefik.enable=false` and no router. Instances with a Domain (Gitea, Vaultwarden, SolidCraft) keep the same labels (#58)
- Postgres template health check was the literal `pg_isready -U {{ .Config.username }}` (health checks are not templates), which exits 3 with "too many command-line arguments", so the container never reported healthy. It now uses `$POSTGRES_USER` and `$POSTGRES_DB` (#58)
- MinIO template `command` was a string GoForge ignored; it is now a list and applied (`server /data --console-address :9001`) (#58)
- Docs claimed Redis used "AOF + RDB" and an optional password; it uses RDB only and the password is required (#58)
- Gitea served its public install page until someone submitted it. It now starts with `INSTALL_LOCK=true`, so the installer never appears, and GoForge creates the admin account (#43)
- A template field missing from an older instance's stored config rendered as `<no value>` in the container environment. It now renders as an empty string (#43)
- Gitea template set `GITEA__APP_NAME`, which Gitea ignores (no section separator). It now sets `GITEA____APP_NAME`
- SSE streaming connections (deployment logs, container stats/logs) killed after 15 seconds by global `WriteTimeout`; replaced with per-route timeout middleware so SSE routes stream indefinitely
- Stale documentation references to old project name "GoCool"/"gocool"
- Outdated security warnings in database schema docs (encryption is now implemented)
- `X-Forwarded-For` header trusted unconditionally allowing IP spoofing in audit logs and rate limiter
- Nil pointer panic in project detail tabs when a project has no environments
- `GetProject` handler silently swallowed non-`sql.ErrNoRows` deployment fetch errors
- Redundant `//nolint:errcheck` directive and misleading comment in settings handler
- CSS spacing inconsistency (`mt-6` inside grid with `gap-6`) on resource usage card

### Security
- Dependency and toolchain bump to clear every reachable `govulncheck` finding: builder image `golang:1.26.10` and `go 1.26.0` in `go.mod` (CI `GO_VERSION` 1.26); go-git v5.19.3, go-billy v5.9.2, golang.org/x/crypto v0.58.0, golang.org/x/net v0.62.0, OpenTelemetry v1.47.0, cloudflare/circl v1.6.5, go-chi/chi v5.3.0. The Docker SDK moves from `github.com/docker/docker` v27.3.1 (GO-2026-4887, GO-2026-4883: no fixed version of that module) to `github.com/moby/moby/client` v0.6.2 and `github.com/moby/moby/api` v1.56.1
- Added trusted proxy validation for `X-Forwarded-For` / `X-Real-IP` headers (configurable via `TRUSTED_PROXIES` env var)

---

## [0.1.0] - 2026-01-11

### Added
- Core project structure
- Database migrations and PostgreSQL support
- GitHub OAuth authentication
- Project and environment management
- Git integration (GitHub, GitLab, Gitea)
- Docker build and deployment pipeline
- Traefik reverse proxy with Let's Encrypt
- Real-time logs via Server-Sent Events
- Resource monitoring (CPU, memory, disk)
- One-click service catalog
- HTMX-powered responsive dashboard
