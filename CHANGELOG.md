# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- Mobile menu did nothing on a phone after the PR6 deploy: the page loaded a cached pre-dialog `ui.js` (Cloudflare and the browser keep unversioned `/static/js/*` for up to 4 h), so the Sheet opener had no handler. Base now loads every script with a content-hash `?v=` like the stylesheet
- Service detail stuck on "Connecting to metrics stream..." after **Edit configuration** or **Update image**: the pending poll sat outside the swapped region and never stopped, so every 3 s swap tore down the stats stream before its first frame. The poll now lives inside `#service-detail-content` and drops out once the service is `running`; the stream element is `hx-preserve`d by Docker ID across pending polls
- `GET /containers/{id}/stats` sends its first `stats` frame about 0.5 s after connect (two one-shot samples, real CPU delta) instead of after the first 2 s tick, and sends a visible `unavailable` event for a stopped or missing container instead of staying silent
- Project Overview streams only a running container: the latest deployment's, else the newest earlier successful deployment's (so stats stay up during a deploy), else an empty state. It no longer falls back to a stopped container
- Network and block I/O are now filled in container metrics (they were always 0, also in `/api/v1` stats)

### Added
- Resource Usage shows network I/O and block I/O
- **Container** card on service detail and project Overview: image, state, health, uptime, restart count, OOM flag, limits, ports, Traefik host rule, and **View logs** (`/containers/{dockerID}`). Uses `docker.SummarizeInspect`, the secret-free inspect mapping now shared with `/api/v1` (JSON unchanged)
- `goforge backup list` and `goforge backup restore <run key>`: restore the GoForge database (`app/goforge-db.dump` only) from a backup run. Refuses a run with no manifest, a newer manifest version, a size or SHA-256 mismatch, a held backup lock, other connected clients (`stop GoForge first: docker compose stop goforge`), or a schema newer than the binary. Loads into a side database (`pg_restore -f -` piped into `psql` with `ON_ERROR_STOP` in one transaction, `SET transaction_timeout` dropped so PostgreSQL 15 servers work), clears sessions (API tokens stay as in the dump), writes a `backup.restore` audit row with the run key only, then renames: the old database is kept as `<db>_pre_restore_<UTC time>` and never dropped. Dry run by default; `--yes` asks for the run key on a TTY or takes `--confirm=<run key>`. Never reads `services/...` objects or touches service containers; no secrets on argv or in output
- Settings → Profile → Backups: read-only Restore points list for admins (`GET /settings/backups/restore-points`, `Cache-Control: no-store`) with the restore command; no restore button
- MCP over streamable HTTP at `/mcp` on `goforge serve` (stateless, JSON responses; go-sdk `StreamableHTTPHandler` behind `http.CrossOriginProtection`), beside `/api/v1`: outside the session and CSRF groups, same 15 s timeout and rate limiter. `Authorization: Bearer` with a Profile token only; no or bad token `401` with `WWW-Authenticate: Bearer realm="goforge-mcp"`, `full` and `protected`-only tokens `403 insufficient_scope`. Tool calls go through the `/api/v1` handler in process with the caller's token, so scope, ownership, protected services, `dry_run`, `approval_ref`, and audit-before-act are unchanged and the audit row has the caller's IP. A read token lists 23 tools; a token with `write` lists 37 (no `GOFORGE_MCP_ALLOW_WRITES` needed); a write tool with a read token is a tool error `insufficient_scope`. On by default; `GOFORGE_MCP_HTTP=false` turns it off
- Settings → Profile → MCP card: the `/mcp` endpoint, a copy-ready `mcp.json` that reads the token from `${env:GOFORGE_MCP_TOKEN}`, and **Generate MCP token** (`read` + `write`, 90 days, never `full`)
- API tokens: new tokens start with `gf_` and store a non-secret hint (`gf_…a1b2`, migration `021_api_token_hint`) shown in the list; **Rotate** puts a new secret on the same row and the old one stops working; create, rotate, and revoke write `api_token.create` / `api_token.rotate` / `api_token.revoke` audit rows without token values. Rotate and revoke of another user's token answer `404`
- Backup settings in Settings → Profile → Backups for admins: every `BACKUP_*` setting plus an Enabled switch, a Test connection button (lists one object under the prefix, 10 s limit), and live apply on save with no restart; a run already in progress keeps its settings. Migration `020_backup_settings` stores them in one row with the access key ID and secret sealed with `ENCRYPTION_KEY`. Once the row exists it wins over `BACKUP_*`; on first start with no row and `BACKUP_S3_BUCKET` set, `BACKUP_*` is copied into it. If the saved keys do not decrypt with the current `ENCRYPTION_KEY`, backups stay off (no fallback to `BACKUP_*`) and the form shows the other settings and asks for both keys again; saving both recovers without a restart. A blank secret keeps the stored one, the secret is never sent back, and the key ID shows at most its last 4 characters. Saves are audited as `backup.settings.update` with changed field names only. `goforge serve`, the status card, `GET /api/v1/backups` (new `configured_by`), and `goforge backup run` all read the same settings. `PUT /settings/backups`, `POST /settings/backups/test` (CSRF, admin only, `Cache-Control: no-store`)
- Nightly backups to S3-compatible storage (R2, B2, MinIO) inside `goforge serve`, default `02:00 America/Chicago`, off while `BACKUP_S3_BUCKET` is empty (#62; t1241u, t1263u). Each run uploads the GoForge database (`pg_dump -Fc`), every running Postgres service (`pg_dump -Fc` per database and `pg_dumpall --roles-only`, as the container's `POSTGRES_USER`), Gitea (SQLite online backup plus a tar of `/data`), and Vaultwarden (`/vaultwarden backup` plus a tar of `/data`), with a `manifest.json` of sizes and SHA-256 sums. Retention keeps the newest `BACKUP_RETENTION_DAILY` (7) complete days and prunes only after a successful run. `ENCRYPTION_KEY` is never uploaded. Backup only; restore is manual (`docs/deployment/backup.md`)
- `goforge backup run`: one backup now, for manual runs and smoke tests
- Migration `019_backup_runs`; Settings → Profile shows the last backup to admins; `GET /api/v1/backups` (read scope, admin) and MCP tool `get_backup_status`
- Runtime image installs `postgresql17-client`
- JSON API `/api/v1` for automation (#61, t1250u): bearer token only, outside the CSRF group, session cookies ignored; JSON errors with stable codes; ownership answers match the UI (#28). Reads mask secret env vars and service config; there is no secret-reveal route. Mutations are dry runs unless `"dry_run": false`, a real run needs `approval_ref`, and an `api_v1.<action>` audit row (approval_ref, token id) is written before anything changes; a failed audit insert blocks the change
- API token scopes `read`, `write`, `protected` (migration `018_api_token_scopes`); chosen on Settings → Profile → API Tokens. Existing and unscoped tokens are `full`. Scoped tokens only work on `/api/v1` (#61, t1250u)
- `GOFORGE_PROTECTED_SERVICES`: service instance IDs whose stop, config edit, and image update through `/api/v1` need the `protected` scope (`403 protected_resource` before any Docker call); restart stays allowed with `write` (#61, t1250u)
- `cmd/goforge-mcp`: MCP server over stdio (go-sdk v1.8.0) with 23 read tools (including `get_backup_status`) and, with `GOFORGE_MCP_ALLOW_WRITES=true`, 14 gated write tools; calls `/api/v1` with a scoped token, holds no Docker socket or database. `make build-mcp`, `make docker-build-mcp`, Dockerfile target `mcp`. Docs: `docs/reference/api-v1.md`, `docs/guides/mcp.md` (#61, t1250u). Now the stdio fallback for `/mcp`, pointed at the HTTPS URL.
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
- `scripts/shared-data/redis-tenant.sh`: creates, rotates, or (`--revoke`) deletes one app's Redis ACL user confined to keys and channels `<app>:*` (`resetchannels`, `+@all -@admin -@dangerous +info -scan -randomkey -pubsub`, plus `-function` on 7.x; works on 6.2 and 7.x), password on stdin and hashed inside the container, saved with `ACL SAVE` so it survives restarts. Refuses, changing nothing, an instance without an ACL file (pre-#60); runs `ACL LOAD` if `ACL SAVE` fails. Not run by GoForge (#60)
- Docs: shared data services (shared-pg / shared-redis with no Domain and no published port) and a shared-pg nightly `pg_dump` runbook (not installed) (#59)
- UI shell: sidebar **Log out** (`POST /auth/logout` with CSRF), a Containers link for all users, and the active page marked with `aria-current="page"` (longest `data-nav` prefix, so deployment pages highlight Projects). A `#toast` live region with `static/js/app.js` turns HTMX response and network errors into toasts (modal requests and `every` polls excepted). Deploy Now, service Restart and Update image, Sync from repo, and the HTMX form submits disable their button and show a spinner while in flight (#65)
- UI components `StatusBadge`, `PageHeader`, `Breadcrumbs`, `Alert` / `AlertMessage`, `LoadingState`, `Skeleton`; CSS `.btn-warning`, `.btn-sm`, `.icon-btn`, `.card-header` / `.card-body` / `.card-title`, `.table-th` / `.table-td`. `Modal` is a labelled dialog that closes on Escape or a backdrop click. Inter is self-hosted (`static/fonts`, OFL), plus `favicon.svg` / `favicon.ico` and a `/favicon.ico` route (#67)
- System dark mode: Tailwind `darkMode: 'media'`, so the UI follows `prefers-color-scheme`; there is no theme toggle. `output.css` is linked as `?v=<first 12 hex of its SHA-256>` to bust caches. CI `css` job builds Tailwind 3.4.17 and runs `scripts/class_check.py`, which fails on literal class tokens missing from `output.css`; `make css-check` runs the same locally (#68)
- `.env.example` lists `GOFORGE_PROTECTED_SERVICES`, `GOFORGE_BUILD_DIR`, and `MAX_CONCURRENT_BUILDS` (accepted, unused) and names the `goforge-mcp` variables (security Goal 4)

### Changed
- `docker-compose.yml` no longer passes `BACKUP_*` into the `goforge` container, so a recreated container has no `BACKUP_*` names in its environment. Compose installs set backups up in Settings → Profile → Backups; `BACKUP_*` in the compose `.env` now has no effect. For a one-time import, add them under `services.goforge.environment` in your own `docker-compose.override.yml`, then remove them. Host runs and `goforge backup run` still read `BACKUP_*` from the process environment (no code change). `.env.example` keeps the block commented out, and the backup, configuration, compose, and `SECURITY.md` docs say the same
- `config.Load` no longer fails on invalid `BACKUP_*`. Serve and `goforge backup run` check it after opening the database: with no saved backup settings it still stops startup with the same message; once saved settings exist it is ignored with a warning, so a partly removed `BACKUP_*` set does not stop GoForge. Other commands (`migrate`) no longer fail on it
- `ROADMAP.md` brought up to date through #71: shipped items checked after checking the code (active sidebar, MySQL/MongoDB/MinIO templates, service stop/start/restart, API tokens, mobile hamburger menu), a "Shipped since V1.0.x" list, and a parked list (#60, #48, backup restore, aesthetics Goal 3 nits, deadcode sweep, stale branches, open audit findings). The rollback-toast item was marked done but never shipped; it is open again (security Goal 4)
- `make css` pins Tailwind v3.4.17 (`TAILWIND_VERSION`) and warns when the CLI on `PATH` is another version; `TAILWIND` can be overridden (for example `TAILWIND="npx --yes tailwindcss@3.4.17"`). `make ci` runs `css-check`. `make install-tools` and the prerequisites doc download v3.4.17 instead of `latest` (v4). CI lint pins golangci-lint v1.64.8, the `go.mod` tool version, instead of `latest` (security Goal 4)
- Docs: CSRF described as HMAC tokens (was double-submit), login lockout and client IP in the authentication doc, `SECURITY.md` lists the open 2026-10-29 audit findings, the Vaultwarden guide says `/admin` (not the service page or `/api/config`) shows whether signups are on, example Dockerfile uses `golang:1.26-alpine` / `alpine:3.24`, `AGENTS.md` lists `make css-check` and `make ci` (security Goal 4)
- Sidebar "Recent projects" section removed (it was always empty); `AppLayout(title, csrfToken)` and `Sidebar(csrfToken)` drop the `projects` parameter. Project detail form fields use `.input` / `.select` (#65)
- Login, register, error, dashboard, deployment, container, variables, stats, and pagination pages use the `.card` / `.btn-*` / `.input` design system; Rollback and Stop are `btn-warning`. An HTMX `RenderError` returns an escaped `AlertMessage` fragment. The status color maps are replaced by `StatusBadge` (#67)
- Docs: `CONTRIBUTING.md` rewritten as the one contributor guide (house rules tied to checks, test types, migrations, service templates, PR format); `docs/development/index.md` points to it; `GEMINI.md` points to `AGENTS.md`; PR template gains Tests, Proof, and Deferred sections; README, schema reference (migrations 016 to 019), project structure, Compose backup sections, and testing docs brought up to date with #61 and #62 (#64, t1275u)
- Router: global middleware is recovery and rate limit only; session and API-token auth wrap the UI routes, and `/api/v1` mounts beside them. `docs/reference/api-routes.md` brought in line with the routes (#61, t1250u)
- Dependencies: `golang.org/x/crypto`, `x/oauth2`, `x/sync`, `x/time`, `x/tools` minor bumps pulled in by `github.com/modelcontextprotocol/go-sdk` v1.8.0 (#61, t1250u)
- PostgreSQL template defaults to 17 (16, 15, 14 still offered), admin username `pgadmin` instead of `postgres`, a 32-char generated password, and 1 CPU / 1GiB; `database`, `username`, and `password` are immutable because the image applies them only on first init (#59)
- Redis template keeps the password in an ACL file instead of `requirepass`: every start writes `user default on #<sha256 of REDIS_PASSWORD> ~* &* +@all` into `/data/users.acl`, keeps the other users in it, and loads it with `aclfile`. An empty password still refuses to start; AOF, `noeviction`, `dir /data`, and the authenticated health check are unchanged. Existing Redis instances switch on their next recreate (config edit or image update); clients using `redis://:<password>@...` keep working (#60)
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

- `make css` and `make css-watch` run Tailwind v3.4.17 only: the standalone `tailwindcss` on `PATH` when it reports v3.4.17, else `npx --yes tailwindcss@3.4.17`. A missing CLI or another version is now an error instead of a warning or "placeholder CSS" with exit 0. `make css-check` deletes `output.css` before rebuilding, so it cannot pass on a stale file (aesthetics Goal 3 nits, #68)
- `scripts/class_check.py` also checks dynamic classes: string literals in templ `class={ ... }` expressions (including `templ.KV` keys), `return "..."` strings in `*Class` Go helpers under `internal/web/templates` (`statusBadgeClass`, `alertClass`, `logLevelBadgeClass`, `backupLineClass`, ...), `Skeleton` arguments, and `static/js/app.js` `LEVEL_CLASSES`, `.className`, and `classList` literals. It fails on an `output.css` with under 200 selectors (placeholder or failed build) (aesthetics Goal 3 nits, #68)
- Error pages are auth-aware (B17): signed-in users get the app shell (sidebar, Log out) and "Back to dashboard"; signed-out visitors get a "Sign in" link instead of `/dashboard`, which only redirected them to `/login`. `errors.ErrorPage`, `NotFound`, `Forbidden`, and `InternalError` take a `signedIn bool` (aesthetics Goal 3 nits)
- `.select` chevron uses gray-400 (`#9ca3af`) in dark mode instead of gray-500 (aesthetics Goal 3 nits, #68)

### Deprecated

### Removed
- `components/nav.templ` (no callers) (#65); `components.Table`, the old `Loading`, and the unused `.glass`, `.glass-dark`, `.btn-ghost` classes (#67)
- `scripts/tailwind.sh`, a duplicate of `make css` (security Goal 4)
- Unused `.sidebar-link`, `.sidebar-link-active`, `.sidebar-link-inactive` CSS; the sidebar styles links inline with `aria-[current=page]` variants, so Tailwind purged these (aesthetics Goal 3 nits, #68)
- Removed orphaned `CustomDomain` model and `internal/domain` logic that had no database repository

### Fixed
- Backup docs: the GoForge database restore no longer says `docker compose exec -T db pg_restore`, which fails on the PostgreSQL 15 `db` container (`unsupported version (1.16) in file header`) for dumps made by the PostgreSQL 17 client
- Settings → Profile → API Tokens no longer says token authentication is planned. It now says how to send a token (`Authorization: Bearer`), that scoped tokens work on `/api/v1` (which the MCP server uses), and that full tokens also work on the web routes
- Two config edits (or an edit and an image update) sent at the same moment could both pass the "still deploying" check and both recreate the container, racing stop, remove, and create. The edit now claims the instance by switching its status to `pending` in one `UPDATE ... WHERE status = <status it read>`; the request that loses gets `409 Conflict` and touches nothing. Volumes were never at risk (#48)
- `GET /containers/api` matched `/containers/{id}` (chi ranks a subrouter catch-all below params), returned 403, and the containers list never loaded; the two routes are flat now (#66)
- Containers list rows were swapped into a `div` inside a `td`; the `tbody` is the target and refreshes after stop and restart (#65)
- Containers Type column was always empty (templ discarded a Go statement); container detail Resource Usage never subscribed to the stats SSE; a successful modal request left its backdrop behind, and a failed one showed raw HTML (#67)
- Undefined classes `input-text` and `no-scrollbar` (#65); sidebar did not stretch to the full column height (#68)
- `RollbackDeployment` had a stale `TODO` for a rollback toast that `ROADMAP.md` called done; the comment now says why the result is not shown (security Goal 4)
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
- Backup keys saved in Settings → Profile are stored sealed with `ENCRYPTION_KEY` instead of as container environment variables readable with `docker inspect`; after the first-start import, remove every `BACKUP_*` line from your env file (`.env`) and recreate the container (ENV-CREDS). The backup redactor also covers the saved keys
- Client IP for the rate limiter, login lockout, audit log, and API: from a `TRUSTED_PROXIES` peer, GoForge now uses the rightmost `X-Forwarded-For` hop that is not a trusted proxy, then `X-Real-IP`. The leftmost `X-Forwarded-For` entry is client-written and is no longer trusted, so rotating it no longer gets a fresh rate-limit bucket. `TRUSTED_PROXIES` is documented in `.env.example` (XFF-SPOOF, #70)
- `CF-Connecting-IP` is read (before `X-Forwarded-For`) only when `CLOUDFLARE_PROXY_MODE=true`. It is off by default because Traefik forwards the header as the client sent it, so a client that reaches Traefik directly could otherwise pick its own client IP. Tunnel deployments should set `CLOUDFLARE_PROXY_MODE=true` so tunnel users do not share cloudflared's rate-limit bucket (CF-SPOOF, #70)
- Login lockout: 10 failed password logins per client IP, or per account from any IP, within 15 minutes lock that key for 15 minutes; `POST /auth/login` answers `429` with `Retry-After` before checking the password (LOGIN-THROTTLE, #70)
- Security headers on every response: `Content-Security-Policy: frame-ancestors 'none'; base-uri 'self'; object-src 'none'`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, and `Strict-Transport-Security: max-age=31536000` on HTTPS (NO-SEC-HEADERS, #70)
- CSRF tokens are HMAC-SHA256 keyed by `CSRF_SECRET` (previously required but unused): session-bound when signed in, signed nonce when signed out. A planted `goforge_csrf` cookie no longer passes. Pages open across the upgrade need one reload (CSRF-SECRET-UNUSED, #70)
- `goforge serve` deletes expired session rows at startup and hourly (SESSION-PURGE, #70)
- Dependency and toolchain bump to clear every reachable `govulncheck` finding: builder image `golang:1.26.10` and `go 1.26.0` in `go.mod` (CI `GO_VERSION` 1.26); go-git v5.19.3, go-billy v5.9.2, golang.org/x/crypto v0.58.0, golang.org/x/net v0.62.0, OpenTelemetry v1.47.0, cloudflare/circl v1.6.5, go-chi/chi v5.3.0. The Docker SDK moves from `github.com/docker/docker` v27.3.1 (GO-2026-4887, GO-2026-4883: no fixed version of that module) to `github.com/moby/moby/client` v0.6.2 and `github.com/moby/moby/api` v1.56.1 (DEPS-VULN, #69)
- Added trusted proxy validation for `X-Forwarded-For` / `X-Real-IP` headers (configurable via `TRUSTED_PROXIES` env var)
- Repo `docker-compose.yml`: the Traefik API and dashboard are off by default (`--api`, `--api.dashboard`, and `--api.insecure` follow `TRAEFIK_DASHBOARD_INSECURE`, default `false`, so `api@internal` does not exist and a label-defined router cannot expose it); the unauthenticated `traefik-dashboard` router (`Host(traefik.${GOFORGE_DOMAIN})`, `web` entrypoint, `api@internal`) is removed; the dashboard port is published on `127.0.0.1:${TRAEFIK_DASHBOARD_PORT:-8081}` instead of `0.0.0.0`. Traefik 80/443 are unchanged (all interfaces, for direct installs); tunnel deployments should bind them to loopback in an override (REPO-COMPOSE-TRAEFIK, #71)
- Pinned images: `cloudflare/cloudflared:2026.6.0` (was `latest`) in Compose, and `alpine:3.24` (was `latest`) for the runtime and `mcp` Dockerfile stages (UNPINNED, partial, #71)
- `SECURITY.md` documents the Docker socket trust model: the read-write socket in the `goforge` container, Traefik's `:ro` socket (which does not limit API calls), and `docker` group membership are root-equivalent on the host; mitigations and the Cloudflare Tunnel + loopback exposure model (DOCKER-SOCK, #71)

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
