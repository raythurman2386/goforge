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

### Partly done

- [ ] **Database encryption at rest.** `ENCRYPTION_KEY` encrypts these values with AES-GCM: project variables marked secret, GitHub OAuth access tokens, Git source access tokens and SSH private keys, and one-click service `config` and `connection_info`. Gaps: rows written in plaintext by an older version stay plaintext until they are updated, non-secret project variables are plaintext, and there is no key rotation.
- [ ] **Role-based access control.** Accounts have a role of `admin` or `user`. Every account is created as `user`, and only the address in `GOFORGE_ADMIN_EMAIL` becomes `admin` (matching ignores case). The admin role gates host operations such as Docker prune. Projects, deployments, and services are scoped to the owning account, and another account's project or deployment returns `404`. Gaps: no teams, invites, or per-project roles.
- [ ] **Audit logging.** An `audit_logs` table exists and some actions, such as login, are recorded. Coverage is not complete.

### Not started

- [ ] Two-factor authentication (2FA)
- [ ] API key rotation mechanism
- [ ] Container escape prevention measures
- [ ] Resource quota enforcement per project
- [ ] `/health/ready` is unauthenticated and its JSON includes database and Docker error text
- [ ] `BACKUP_S3_*` credentials set in your env file (`.env`) are plain environment variables in the `goforge` container, so anyone who can run `docker inspect` (the `docker` group) can read them. Backup keys saved in Settings → Profile are stored in the database encrypted with `ENCRYPTION_KEY` and never sent back to the browser; after the first-start import, remove `BACKUP_S3_ACCESS_KEY_ID` and `BACKUP_S3_SECRET_ACCESS_KEY` from your env file to close this
- [ ] The main `goforge` image runs as root (the `mcp` image runs as uid 10001)

### Defaults that reduce exposure

- Registration is closed after the first account unless `GOFORGE_ENABLE_REGISTRATION=true`.
- `GOFORGE_DEV_MODE` defaults to `false`. Outside dev mode GoForge refuses to start without `DATABASE_URL`, `ENCRYPTION_KEY`, `SESSION_SECRET`, and `CSRF_SECRET`.
- Docker Compose publishes the app on `127.0.0.1` only. Postgres is not published on the host.
- The Traefik API and dashboard are off: `--api`, `--api.dashboard`, and `--api.insecure` all follow `TRAEFIK_DASHBOARD_INSECURE` (default `false`), so the `api@internal` service does not exist and no router, including one added through container labels, can expose it. `TRAEFIK_DASHBOARD_INSECURE=true` turns them on for local debugging only, on `127.0.0.1:${TRAEFIK_DASHBOARD_PORT:-8081}`. No router in the repo compose file publishes `api@internal` on the web or websecure entrypoints.
- Image tags are pinned: `cloudflare/cloudflared:2026.6.0` in Compose and `alpine:3.24` for the runtime and MCP images.
- Traefik on 80 and 443 is published on all interfaces, for direct (LAN or public IP with Let's Encrypt) installs. With Cloudflare Tunnel those host ports are not needed; bind them to `127.0.0.1` or drop them in a `docker-compose.override.yml`.

## Docker socket trust model

GoForge builds images and creates, stops, and removes containers through the Docker Engine API. `docker-compose.yml` mounts `/var/run/docker.sock` read-write into the `goforge` container, and the app runs as root inside it.

- **The Docker API is root on the host.** Anything that can send requests to that socket can start a privileged container, bind-mount `/`, and act as root on the host. A compromise of the GoForge process, or of anything that can execute inside its container, is a host compromise.
- **`:ro` does not make the socket read-only.** Traefik's mount is `/var/run/docker.sock:/var/run/docker.sock:ro`. That stops the socket file from being replaced; it does not limit which API calls go through it. Treat Traefik's socket access as equally powerful. Traefik uses it to read container labels for routing.
- **`docker` group membership is root-equivalent.** Add only people who would also get `sudo`. Anyone who can run `docker exec` in the `goforge` or `traefik` container also holds the socket.
- **Only `goforge` and `traefik` hold the socket.** The MCP server image (`--target mcp`) runs as an unprivileged user with no socket, no database, and no published port. GoForge does not mount the socket into the project and one-click service containers it deploys.

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
- [ ] Use strong, unique secrets for all credentials
- [ ] Restrict access to Docker socket (see "Docker socket trust model" above)
- [ ] Enable firewall rules (allow outbound to Cloudflare, block unnecessary inbound)
- [ ] Regular security updates and patches

### Recommended Hardening
- [ ] Review audit log coverage (partial today)
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
