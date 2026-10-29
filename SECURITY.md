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

### Defaults that reduce exposure

- Registration is closed after the first account unless `GOFORGE_ENABLE_REGISTRATION=true`.
- `GOFORGE_DEV_MODE` defaults to `false`. Outside dev mode GoForge refuses to start without `DATABASE_URL`, `ENCRYPTION_KEY`, `SESSION_SECRET`, and `CSRF_SECRET`.
- Docker Compose publishes the app on `127.0.0.1` only. Traefik on 80 and 443 is the public entrypoint. Postgres is not published on the host.

## Security Best Practices for Deployment

### Minimum Requirements
- [ ] Use **Cloudflare Tunnel** for external access (recommended - no inbound ports needed)
- [ ] Traefik handles internal container routing only (not exposed publicly)
- [ ] Use strong, unique secrets for all credentials
- [ ] Restrict access to Docker socket
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
