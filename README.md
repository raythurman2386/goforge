# GoForge

> **⚠️ Security Warning: This project is in active development.** 
> 
> GoForge is a self-hosted PaaS that runs containerized workloads on your infrastructure. 
> Like any system that handles authentication, secrets, and container execution, it requires 
> careful security considerations before production use. See [Security Considerations](#security-considerations) below.

---

GoForge is a lightweight, self-hosted Platform as a Service (PaaS) built in Go.

## Features

- **Authentication:** Local email/password and GitHub OAuth. Registration closes after the first account unless you open it. Accounts are `user` until `GOFORGE_ADMIN_EMAIL` grants `admin`.
- **Project Management:** CRUD operations for projects and environments.
- **Docker Integration:** Build and deploy containers directly from git repositories.
- **Traefik Integration:** Automatic routing with Docker labels (internal only).
- **Cloudflare Tunnel:** Secure public access without opening inbound ports.
- **Monitoring:** Live container CPU, memory, network I/O, and block I/O over SSE, plus a Container card (image, state, health, limits, ports, Traefik host) on service detail and project Overview. Metrics are not stored.
- **Real-time Updates:** SSE-powered deployment logs and status updates.
- **Web UI:** server-rendered templ pages with HTMX 2 and Tailwind CSS 3.4, built from one component kit (`internal/web/templates/components/ui`: native `<dialog>` and `popover`, one delegated `static/js/ui.js`, no inline event handlers). Dark mode follows the OS. Dialogs, menus, tabs, and the mobile navigation are keyboard accessible, and the axe scan of the main pages reports no serious or critical WCAG 2.2 AA issues in light or dark. See [UI kit](docs/reference/ui-kit.md).
- **One-click Services:** PostgreSQL, Redis, Gitea, Vaultwarden, MinIO, and more from YAML templates in `templates/`, including shared Postgres and Redis for several apps. See [Services](docs/guides/services.md).
- **API v1 and MCP:** a bearer-token JSON API at `/api/v1` with scoped tokens and gated, audited writes, and the same MCP tools at `/mcp` (streamable HTTP, scoped Profile tokens, set up from Settings → Profile → MCP), with `goforge-mcp` as a stdio fallback. See [API v1](docs/reference/api-v1.md) and [MCP server](docs/guides/mcp.md).
- **Nightly Backups:** the GoForge database and Postgres, Gitea, and Vaultwarden services uploaded to S3-compatible storage (R2, B2, MinIO) every night, plus `goforge backup run` on demand. `goforge backup restore` puts the GoForge database back from a run; service data restore is a manual runbook. See [Backup & Restore](docs/deployment/backup.md).

## Getting Started

### Quickstart with Docker Compose

This is the supported way to run GoForge. You need Docker Engine with the Compose plugin and `openssl`.

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/raythurman2386/goforge.git
    cd goforge
    ```

2.  **Create `.env` from the example:**
    ```bash
    cp .env.example .env
    ```

3.  **Fill in the required values in `.env`:**
    - `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, and `WEBHOOK_SECRET`: generate each with `openssl rand -base64 32`. Compose refuses to start while the first three are empty, and GoForge refuses to start outside dev mode while `WEBHOOK_SECRET` is empty.
    - `POSTGRES_PASSWORD`: set a real password and use the same value in `DATABASE_URL`.
    - `GOFORGE_ADMIN_EMAIL`: the email of the operator account. Matching ignores case. Leave it empty and nobody is admin.
    - Leave `GOFORGE_DEV_MODE=false`.

4.  **Start the stack:**
    ```bash
    docker compose up -d --build
    curl -s http://localhost:8080/health/ready
    ```
    Ready returns `"database":"ok"` once migrations have run. Migrations run automatically at startup.

5.  **Create the first account:** open `http://localhost:8080` and register. Use the `GOFORGE_ADMIN_EMAIL` address to get the admin role. If you set `GOFORGE_ADMIN_EMAIL` after that account exists, restart GoForge and startup promotes it.

### What the defaults do

- **Registration:** the first account can always register. After that, `/auth/register` returns `403` unless `GOFORGE_ENABLE_REGISTRATION=true`.
- **Admin role:** every account is created as `user`. The account whose email matches `GOFORGE_ADMIN_EMAIL` becomes `admin`, at account creation or at the next startup. The admin role is required for host operations such as Docker prune.
- **Ports:** Compose publishes the app on `127.0.0.1:8080` only. Traefik on ports 80 and 443 (all interfaces) is the entrypoint for direct installs, which need them for Let's Encrypt. With Cloudflare Tunnel, cloudflared reaches Traefik over the Docker network, so bind 80 and 443 to `127.0.0.1` (or drop them) in a `docker-compose.override.yml`. Postgres is not published on the host.
- **Dev mode:** `GOFORGE_DEV_MODE` defaults to `false`. Dev mode generates missing secrets and relaxes config validation. Turn it on only on a local machine.

### Developing from source

Prerequisites: Go 1.26+, Docker Engine, a PostgreSQL you can reach with `DATABASE_URL`, and `air` for hot reload (`make install-tools`). `templ` and `golangci-lint` run through `go tool`.

```bash
go mod download
make migrate   # uses DATABASE_URL
make dev       # sets GOFORGE_DEV_MODE=true for this local process only
```

`make dev` and `make run` turn on dev mode for that one process, so you do not export `GOFORGE_DEV_MODE` yourself. Never copy a dev-mode environment to a server. The server listens on `http://localhost:8080`.

## Commands

- `make build`: Build the production binary.
- `make build-mcp`: Build the `goforge-mcp` binary.
- `make dev`: Run with hot reload (requires `air`).
- `make test`: Run all tests.
- `make lint`: Run golangci-lint.
- `make check`: Run fmt-check, vet, lint, and test.
- `make ci`: Run the full local CI pipeline (deps, templ, css-check, then the `make check` steps). Run it before you push.
- `make templ`: Generate templ templates.
- `make css`: Build `static/css/output.css` with Tailwind v3.4.17 (`make css-check` also fails on class names missing from it).

## Production Deployment

To run GoForge in production using Systemd:

1.  **Build the binary:**
    ```bash
    make build
    ```

2.  **Prepare the directory:**
    ```bash
    sudo mkdir -p /opt/goforge
    sudo cp bin/goforge /opt/goforge/
    sudo cp -r static /opt/goforge/
    sudo cp .env.example /opt/goforge/.env
    # Edit /opt/goforge/.env with your production secrets
    ```

3.  **Install the Service:**
    ```bash
    sudo cp deploy/goforge.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now goforge
    ```

## Architecture

GoForge follows a clean architecture with:
- **Cmd:** Entry point and CLI commands.
- **Internal:** Core logic separated into `auth`, `project`, `docker`, `git`, `deploy`, `proxy`, `services`, `backup`, `apiv1`, `mcpserver`, `sse`, and `monitor`.
- **Web:** HTMX + templ + Tailwind frontend on the `components/ui` kit, with a RESTful-ish backend. `/api/v1` (JSON) and `/mcp` (MCP over HTTP) sit beside it.
- **Database:** Repository pattern supporting PostgreSQL.

## Security Considerations

### ⚠️ Important Security Notes

1. **Not Production-Ready for Untrusted Environments**
   - GoForge is designed for teams/individuals hosting their own applications
   - Do not expose to untrusted users without proper review
   - Review and test thoroughly before any production deployment

2. **Secret Management**
   - Never commit `.env` files or secrets to version control
   - Use strong, unique secrets for `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, and `WEBHOOK_SECRET`. Outside dev mode GoForge refuses to start with an empty `WEBHOOK_SECRET`, since an empty secret would let anyone forge a webhook
   - Rotate credentials regularly
   - Consider using Docker secrets or external secret stores

3. **Network Security**
   - GoForge is designed to work with **Cloudflare Tunnel** (recommended)
   - Compose publishes the app on `127.0.0.1` only. Traefik on 80 and 443 is the entrypoint for direct installs (Let's Encrypt). With Cloudflare Tunnel, bind 80 and 443 to `127.0.0.1` in a `docker-compose.override.yml`
   - No inbound ports needed - tunnel connects outbound to Cloudflare
   - Restrict access to the Docker socket: the `goforge` container's read-write socket, Traefik's socket, and `docker` group membership are root-equivalent on the host (see the Docker socket trust model in [SECURITY.md](SECURITY.md))
   - Use TLS/SSL for all external connections (handled by Cloudflare)
   - Configure firewall to allow only outbound connections to Cloudflare IPs

4. **Container Security**
   - Review deployed container images for vulnerabilities
   - Avoid running containers as root where possible (the `goforge` image itself still runs as root)
   - Implement resource limits to prevent resource exhaustion
   - Regularly update base images and dependencies

5. **Authentication & Authorization**
   - Registration is closed after the first account by default (`GOFORGE_ENABLE_REGISTRATION=false`)
   - Two roles exist: `admin` and `user`. Only `GOFORGE_ADMIN_EMAIL` grants `admin`. There are no teams or per-project roles yet
   - A project or deployment owned by another account returns `404`
   - Use strong passwords for local accounts. Failed logins lock the client IP or the account for 15 minutes after 10 tries in 15 minutes; set `TRUSTED_PROXIES` (and `CLOUDFLARE_PROXY_MODE=true` behind Cloudflare Tunnel) so the lockout and rate limiter see the real client IP
   - Review OAuth permissions requested by GitHub integration
   - There is no 2FA
   - API and MCP tokens are hashed, carry a scope (`full`, `read`, `write`, or `protected`), expire, and can be rotated or revoked in Settings → Profile
   - Audit logs cover sign-in, registration, logout, API token changes, backup settings, restore, and every `/api/v1` and `/mcp` write. Changes made in the web UI are not recorded yet

6. **Data Protection**
   - Turn on the built-in nightly backups (Settings → Profile → Backups) or run your own. See [Backup & Restore](docs/deployment/backup.md). `goforge backup restore` restores the GoForge database; service data restore is manual
   - `ENCRYPTION_KEY` encrypts these values at rest with AES-GCM: project variables marked secret, GitHub OAuth access tokens, Git source access tokens and SSH private keys, one-click service `config` and `connection_info`, and the backup S3 keys saved in Settings
   - Rows written in plaintext by an older version stay readable and are encrypted the next time they are updated
   - Non-secret project variables are stored in plaintext
   - Back up `ENCRYPTION_KEY`. Without it the encrypted values cannot be read

### Reporting Security Issues

If you discover a security vulnerability, please follow our [Security Policy](SECURITY.md). It also lists the open known gaps. The security model (sessions, CSRF, headers, client IP, webhooks, tokens) is in [docs/security.md](docs/security.md).

---

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a PR. AI agents start at [AGENTS.md](AGENTS.md).
