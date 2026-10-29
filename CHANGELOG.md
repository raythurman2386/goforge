# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
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

### Changed
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
