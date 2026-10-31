# Security Policy

## Supported Versions

We recommend always using the latest stable release. Older versions may contain known vulnerabilities.

| Version | Supported          |
| ------- | ------------------ |
| latest  | ✅ Active development |

## ⚠️ Important Disclaimers

### Not Production-Ready
GoForge is currently in active development and has not been audited for security. Before deploying:

- Review the code yourself
- Test in a non-production environment
- Understand the security implications of running a self-hosted PaaS

### Security Responsibilities
When running GoForge, you are responsible for:
- Proper network configuration and firewall rules
- Managing secrets and credentials securely
- Regular updates and patching
- Container image security
- Access control and authentication

## Reporting a Vulnerability

### For Security Researchers
We take security vulnerabilities seriously. If you find a security issue:

1. **Do NOT** open a public GitHub issue
2. Email security concerns privately (or use GitHub's private vulnerability reporting)
3. Include detailed steps to reproduce the issue
4. Allow reasonable time for response before public disclosure

### What to Expect
- Acknowledgment of your report within 48 hours
- Regular updates on progress
- Credit in the security advisory (if desired)

## Known Security Gaps (As of Current Version)

Checked against `main` on 2026-10-30.

### Partly done

- [ ] **Database encryption at rest.** `ENCRYPTION_KEY` encrypts these values with AES-GCM: project variables marked secret, GitHub OAuth access tokens, Git source access tokens and SSH private keys, one-click service `config` and `connection_info`, and the S3 access key ID and secret saved in Settings → Profile → Backups. API tokens and session tokens are stored only as SHA-256 hashes. Gaps: rows written in plaintext by an older version stay plaintext until they are updated, non-secret project variables are plaintext, and there is no `ENCRYPTION_KEY` rotation.
- [ ] **Role-based access control.** Accounts have a role of `admin` or `user`. Every account is created as `user`, and only the address in `GOFORGE_ADMIN_EMAIL` becomes `admin` (matching ignores case). The admin role gates host operations such as Docker prune and the backup settings. Projects, deployments, and services are scoped to the owning account, and another account's project or deployment returns `404`. Gaps: no teams, invites, or per-project roles.
- [ ] **Audit logging.** `audit_logs` records login (password and GitHub), registration, logout, API token create, rotate, and revoke, backup settings saves, `goforge backup restore`, and every `/api/v1` and `/mcp` write (recorded before it acts). Changes made in the web UI to projects, deployments, variables, and services are not recorded.
- [ ] **Sessions.** Each login creates a new random session token, so a session cookie set before login is never promoted. Gaps: changing the password does not end the account's other sessions, and there is no limit on concurrent sessions.

### Not started

- [ ] Two-factor authentication (2FA)
- [ ] Container escape prevention measures
- [ ] Resource quota enforcement per project (per-container CPU and memory limits exist)
- [ ] `/health/ready` is unauthenticated and its JSON includes database and Docker error text (HEALTH-READY-PUBLIC)
- [ ] The main `goforge` image runs as root (the `mcp` image runs as uid 10001)
- [ ] Webhook routes are not limited to a provider's IP ranges; the signature or token is the only check

### Closed since the 2026-10-29 audit

- **`BACKUP_S3_*` in the container environment (ENV-CREDS).** The repo `docker-compose.yml` passes no `BACKUP_*` variable (#80). Backup keys saved in Settings → Profile are stored encrypted with `ENCRYPTION_KEY` and never sent back to the browser (#77). On 2026-10-30 `docker inspect` of the deployed `goforge` container listed no `BACKUP_*` names. If you import settings through a `docker-compose.override.yml`, remove every `BACKUP_*` variable from it afterwards (not only the two keys) and recreate the `goforge` container. Host runs and `goforge backup run` still read `BACKUP_*` from the process environment.
- **API token rotation.** Settings → Profile can rotate a token in place; the old secret stops working on its next request (#78).
- **Empty `WEBHOOK_SECRET` outside dev mode.** With it empty, anyone could sign a GitHub or Gitea webhook (HMAC with an empty key) or send an empty `X-Gitlab-Token`, and a matching push could trigger a deployment. GoForge now refuses to start outside dev mode while `WEBHOOK_SECRET` is empty or only whitespace (#92). Dev mode still generates it.
- **Client IP spoofing, login lockout, CSRF, and session purge** (#70). See [docs/security.md](docs/security.md).

### Defaults that reduce exposure

- Registration is closed after the first account unless `GOFORGE_ENABLE_REGISTRATION=true`.
- `GOFORGE_DEV_MODE` defaults to `false`. Outside dev mode GoForge refuses to start without `DATABASE_URL`, `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, and `WEBHOOK_SECRET`.
- Docker Compose publishes the app on `127.0.0.1` only. Postgres is not published on the host.
- The Traefik API and dashboard are off: `--api`, `--api.dashboard`, and `--api.insecure` all follow `TRAEFIK_DASHBOARD_INSECURE` (default `false`), so the `api@internal` service does not exist and no router, including one added through container labels, can expose it. `TRAEFIK_DASHBOARD_INSECURE=true` turns them on for local debugging only, on `127.0.0.1:${TRAEFIK_DASHBOARD_PORT:-8081}`. No router in the repo compose file publishes `api@internal` on the web or websecure entrypoints.
- Image tags are pinned: `cloudflare/cloudflared:2026.6.0` in Compose and `alpine:3.24` for the runtime and MCP images.
- Traefik on 80 and 443 is published on all interfaces, for direct (LAN or public IP with Let's Encrypt) installs. With Cloudflare Tunnel those host ports are not needed; bind them to `127.0.0.1` or drop them in a `docker-compose.override.yml`.

## Docker socket trust model

GoForge builds images and creates, stops, and removes containers through the Docker Engine API. `docker-compose.yml` mounts `/var/run/docker.sock` read-write into the `goforge` container, and the app runs as root inside it.

- **The Docker API is root on the host.** Anything that can send requests to that socket can start a privileged container, bind-mount `/`, and act as root on the host. A compromise of the GoForge process, or of anything that can execute inside its container, is a host compromise.
- **`:ro` does not make the socket read-only.** Traefik's mount is `/var/run/docker.sock:/var/run/docker.sock:ro`. That stops the socket file from being replaced; it does not limit which API calls go through it. Treat Traefik's socket access as equally powerful. Traefik uses it to read container labels for routing.
- **`docker` group membership is root-equivalent.** Add only people who would also get `sudo`. Anyone who can run `docker exec` in the `goforge` or `traefik` container also holds the socket.
- **Only `goforge` and `traefik` hold the socket.** The MCP tools are served by `goforge` itself at `/mcp` and reach Docker only through the `/api/v1` handler and its checks; they add no socket mount. The optional stdio fallback image (`--target mcp`) runs as an unprivileged user with no socket, no database, and no published port. GoForge does not mount the socket into the project and one-click service containers it deploys.

What reduces the blast radius:

- Run GoForge on a host (or VM) dedicated to it and the apps it deploys.
- Put a Docker API filtering proxy (for example Tecnativa's `docker-socket-proxy`) in front of Traefik, which only needs read access to containers and networks. GoForge itself needs build, container, image, network, and volume write calls, so a proxy limits it less.
- Rootless Docker or user-namespace remapping limits what host root means for the daemon. GoForge has not been tested on rootless Docker.
- Keep the web UI behind the intended exposure model: **Cloudflare Tunnel** to Traefik over the Docker network, with host ports bound to `127.0.0.1` (or not published) via a `docker-compose.override.yml`. Then nothing on the host's public interfaces reaches Traefik or GoForge.

## Security Best Practices for Deployment

### Minimum Requirements
- [ ] Use **Cloudflare Tunnel** for external access (recommended - no inbound ports needed)
- [ ] Traefik handles internal container routing only (host ports 80/443 bound to `127.0.0.1` or unpublished in an override)
- [ ] Keep the Traefik dashboard off (`TRAEFIK_DASHBOARD_INSECURE=false`, the default)
- [ ] Use strong, unique secrets for all credentials, including `WEBHOOK_SECRET`
- [ ] Restrict access to Docker socket (see "Docker socket trust model" above)
- [ ] Enable firewall rules (allow outbound to Cloudflare, block unnecessary inbound)
- [ ] Regular security updates and patches

### Recommended Hardening
- [ ] Review audit log coverage (web UI changes are not recorded today)
- [ ] Implement network segmentation
- [ ] Use secrets management (Vault, AWS Secrets Manager, etc.)
- [ ] Regular penetration testing
- [ ] Container image scanning in CI/CD

## Third-Party Dependencies

GoForge depends on several third-party libraries. We monitor for vulnerabilities and update dependencies regularly, but users should:
- Monitor CVE announcements for dependencies
- Keep Go version updated
- Review container base images for vulnerabilities
- Use tools like Trivy or Snyk for scanning

## License & Liability

This software is provided "as is" without warranty of any kind. Users assume all risks associated with deployment and operation of this self-hosted platform.
