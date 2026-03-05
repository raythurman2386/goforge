# Security

> **⚠️ Warning: GoForge is in active development.** 
> This document describes the current security model. Some features listed as "planned" or "known limitations" should be addressed before production use. See the [Security Policy](../SECURITY.md) for full disclaimers.

---

This page documents GoForge's security model, current protections, and known areas for improvement.

---

## Authentication

### Email/Password

- Passwords are hashed using **Argon2id** before storage
- Login is handled via `POST /auth/login` with form-encoded credentials
- Sessions are created upon successful authentication

### GitHub OAuth

- OAuth flow initiated at `/auth/github`
- Callback at `/auth/github/callback` exchanges the code for an access token
- GitHub user ID is linked to the local user account
- Access tokens are stored in the `users` table for API access

The OAuth flow validates the `state` parameter using a cookie-based approach: a UUID is generated, stored in a `github_oauth_state` cookie (HttpOnly, 5-minute expiry), and verified on callback (`internal/web/handlers/auth.go:157-179`).

---

## Session Management

- Sessions use cryptographically random tokens (32 bytes, base64url-encoded)
- Session tokens are **hashed with SHA-256** before storage in the database
- Only the hash is stored; the raw token exists only in the client cookie
- Session cookies use:
    - `HttpOnly: true` - Not accessible to JavaScript
    - `SameSite: Lax` - Prevents cross-site request forgery for most cases
    - `Path: /`
- Expired sessions are cleaned up on access

### Known Limitations

- **No session rotation after login**: The session token is not regenerated after authentication, which creates a session fixation risk if an attacker can set the session cookie before the user logs in
- **No concurrent session limits**: Users can have unlimited active sessions

---

## CSRF Protection

GoForge implements double-submit cookie CSRF protection:

1. A random CSRF token is generated and stored in a cookie (`goforge_csrf`)
2. State-changing requests (`POST`, `PUT`, `DELETE`, `PATCH`) must include the token via:
    - HTTP header: `X-CSRF-Token` (used by HTMX)
    - Form field: `csrf_token` (used by HTML forms)
3. The server compares the submitted token against the cookie value

**Exemptions**: Webhook routes (`/webhooks/*`) are excluded from CSRF protection as they are machine-to-machine endpoints.

### Known Limitations

- **Timing attack**: Token comparison uses `!=` (string comparison) instead of `crypto/subtle.ConstantTimeCompare()`. While exploitation is difficult over a network, constant-time comparison is the standard practice (`internal/auth/csrf.go:54`)
- **Cookie missing `Secure` flag**: The CSRF cookie does not set `Secure: true`, meaning it could be transmitted over plain HTTP in mixed-content scenarios
- **Token not bound to session**: The CSRF token is independent of the user session, which means a valid CSRF token from one session could theoretically be used in another

---

## Authorization

- Route-level authorization is enforced by the `RequireAuth` middleware
- Users can only access their own resources (projects, environments, services)
- Resource ownership is validated at the repository layer via `user_id` filtering

!!! note
    GoForge currently has a single-role model (all authenticated users have the same permissions). There is no admin/user role distinction.

---

## Secrets & Credential Storage

### Environment Variables

- Environment variables marked as `is_secret` are encrypted at rest using AES-256-GCM via the `secrets.EncryptionService` (`internal/database/repositories/env_variable.go`)
- The `ENCRYPTION_KEY` environment variable provides the key material
- Variables are decrypted at read time and injected into containers at deploy time via Docker environment variables

### Git Source Credentials

- Git provider access tokens and SSH private keys are stored in the `git_sources` table
- These are encrypted at rest using the same `secrets.EncryptionService` (`internal/database/repositories/git_source.go`)
- GitHub user access tokens in the `users` table are also encrypted (`internal/database/repositories/user.go`)

### Recommendations

- Rotate `ENCRYPTION_KEY` periodically and implement key rotation tooling
- Use a key management system or derive encryption keys from a master secret
- Consider integrating with external secret managers (HashiCorp Vault, etc.)

---

## Network Security

### Recommended: Cloudflare Tunnel

GoForge is designed to work with **Cloudflare Tunnel** for secure public access:

- **No inbound ports required** - The tunnel connects outbound to Cloudflare
- **Zero-trust access** - Traffic is authenticated before reaching your server
- **Automatic TLS** - All connections encrypted end-to-end
- **DDoS protection** - Cloudflare handles attack mitigation

### Traefik (Internal Routing Only)

- Traefik handles **internal container routing** only (not exposed publicly)
- All external traffic routes through Cloudflare Tunnel first
- Automatic SSL/TLS via Let's Encrypt (ACME) for custom domains
- Containers are not directly exposed to the internet
- Internal communication happens over the Docker network

### Rate Limiting

- Global rate limit: **30 requests/second** with a burst of **60**
- Applied to all routes via middleware
- Uses a token bucket algorithm

### Docker Network Isolation

- Deployed application containers run on the `goforge` Docker network
- Service instances (PostgreSQL, Redis) are only accessible from within the Docker network
- Connection strings use internal Docker DNS names

---

## Webhook Security

Webhooks receive push events from git providers to trigger deployments.

| Provider | Path | Verification |
|----------|------|--------------|
| GitHub | `/webhooks/github` | Signature verification (`X-Hub-Signature-256`) should be implemented |
| GitLab | `/webhooks/gitlab` | Token-based verification |
| Gitea | `/webhooks/gitea` | Token-based verification |

!!! warning "Improvement Needed"
    Webhook signature verification should be thoroughly reviewed to ensure all providers properly validate the request origin. Unsigned webhooks could allow anyone to trigger deployments.

---

## Docker Security

### Build Process

- Images are built using the Docker SDK (`docker/build.go`)
- Build context is sent from cloned repository contents
- Dockerfiles are user-provided and could contain arbitrary instructions

### Runtime

- Containers run with default Docker security settings
- Resource limits (CPU, memory) are configurable per environment
- The GoForge application itself runs as root in the Docker container

### Recommendations

- Run the GoForge container as a non-root user
- Consider using Docker Content Trust for image verification
- Implement a Dockerfile linter/scanner in the build pipeline
- Add network policies to restrict inter-container communication

---

## Database Security

- PostgreSQL connections use the credentials in the `DATABASE_URL` environment variable
- The default Docker Compose configuration exposes PostgreSQL port 5432 to the host

### Recommendations

- Remove the host port mapping for PostgreSQL in production (`5432:5432` -> internal only)
- Use strong, unique database credentials (not the default `password`)
- Enable SSL for PostgreSQL connections in production
- Restrict database user privileges to only what GoForge needs

---

## HTTP Server Configuration

| Setting | Value | Notes |
|---------|-------|-------|
| Read Timeout | 15s | |
| Write Timeout | 15s | May kill SSE connections prematurely |
| Idle Timeout | 60s | |
| Max Header Bytes | 1MB | |

---

## Security Checklist for Production

### Before Production Use

> ⚠️ **Important**: GoForge is in active development. Review and address the following before production deployment:

- [ ] Understand that the project has not been security-audited
- [ ] Test thoroughly in a non-production environment
- [ ] Review all known limitations in this document

### Deployment Checklist

- [ ] Set strong, unique values for `SESSION_SECRET` and `CSRF_SECRET`
- [ ] Use a strong `DATABASE_URL` password (not the default)
- [ ] Configure `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` if using OAuth
- [ ] **Use Cloudflare Tunnel** for external access (recommended)
- [ ] Enable HTTPS via Traefik with valid SSL certificates
- [ ] Remove PostgreSQL host port mapping in Docker Compose
- [ ] Run GoForge as a non-root user
- [ ] Set up regular database backups
- [ ] Review and restrict Docker socket access
- [ ] Configure firewall rules to limit access (allow outbound to Cloudflare only)
- [ ] Set up log monitoring and alerting for security events
