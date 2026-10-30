# Security

> **⚠️ Warning: GoForge is in active development.** 
> This document describes the current security model. Some features listed as "planned" or "known limitations" should be addressed before production use. See the [Security Policy](https://github.com/raythurman2386/goforge/blob/main/SECURITY.md) for full disclaimers.

---

This page documents GoForge's security model, current protections, and known areas for improvement.

---

## Authentication

### Email/Password

- Passwords are hashed using **Argon2id** before storage
- Login is handled via `POST /auth/login` with form-encoded credentials
- Sessions are created upon successful authentication
- Failed password logins are throttled in memory: 10 failures from one client IP, or 10 failures for one account (email, case-insensitive) from any IP, within 15 minutes lock that key for 15 minutes. A locked login gets `429` with `Retry-After` before the password is checked, so a correct password is refused until the lockout ends. A successful login clears the account counter, not the IP counter. The account limit means someone who knows an email can keep it locked; the lockout is short for that reason. Counters reset when the process restarts

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
- Expired sessions are deleted on access, and `goforge serve` deletes every expired session row at startup and then hourly

### Known Limitations

- **No session rotation after login**: The session token is not regenerated after authentication, which creates a session fixation risk if an attacker can set the session cookie before the user logs in
- **No concurrent session limits**: Users can have unlimited active sessions

---

## CSRF Protection

State-changing requests (`POST`, `PUT`, `DELETE`, `PATCH`) in the UI group must send a CSRF token in the `X-CSRF-Token` header (HTMX, via `hx-headers`) or the `csrf_token` form field. Tokens are HMAC-SHA256 values keyed by `CSRF_SECRET` (`internal/auth/csrf.go`):

1. **Signed in**: the token is `HMAC(CSRF_SECRET, SHA-256(session token))`. It is stable for the session, fails in any other session, and cannot be computed without both the session cookie and the secret.
2. **Signed out** (login and register forms): the token is a random nonce plus `HMAC(CSRF_SECRET, nonce)`, stored in the `goforge_csrf` cookie and echoed in the form.
3. The token is compared in constant time. A `goforge_csrf` cookie without a valid MAC, for example one planted by an app on a sibling subdomain, is replaced and the request gets `403 invalid CSRF token`.

The `goforge_csrf` cookie is not HttpOnly, is `SameSite=Lax`, and is `Secure` on HTTPS requests. Rotating `CSRF_SECRET` invalidates tokens in open pages; reload to get a new one.

**Exemptions**: Webhook routes (`/webhooks/*`) and `/api/v1/*` (bearer only, cookies ignored) are outside the CSRF group.

---

## Security Headers

Every response carries (`internal/web/middleware/secheaders.go`):

| Header | Value |
|--------|-------|
| `Content-Security-Policy` | `frame-ancestors 'none'; base-uri 'self'; object-src 'none'` |
| `X-Frame-Options` | `DENY` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Strict-Transport-Security` | `max-age=31536000`, only when the request is HTTPS (`X-Forwarded-Proto: https` or TLS) |

The CSP does not restrict scripts or styles because the templates use inline scripts and HTMX attributes; it only blocks framing (clickjacking), `<base>` rewrites, and plugins. HSTS leaves out `includeSubDomains` because deployed apps live on sibling subdomains GoForge does not control.

---

## Authorization

- Route-level authorization is enforced by the `RequireAuth` middleware
- Users can only access their own resources (projects, environments, services)
- Resource ownership is validated at the repository layer via `user_id` filtering

Accounts have a `role` of `admin` or `user`. Registration creates users, including the first account. `GOFORGE_ADMIN_EMAIL` promotes that address on process start and when the account is created. The match is case-insensitive, and account emails are stored in lowercase. Host actions such as Docker prune require the admin role and return 403 for anyone else. Project and service ownership is still checked separately, so a user only sees their own resources. Deployment get, rollback, cancel, and status also require the deployment's environment to belong to the project in the path; a cross-project id is not found.

---

## Secrets & Credential Storage

### Environment Variables

- Environment variables marked as `is_secret` are encrypted at rest using AES-256-GCM via the `secrets.Encryptor` (`internal/database/repositories/env_variable.go`)
- The `ENCRYPTION_KEY` environment variable provides the key material
- Variables are decrypted at read time and injected into containers at deploy time via Docker environment variables

### Git Source Credentials

- Git provider access tokens and SSH private keys are stored in the `git_sources` table
- These are encrypted at rest using the same `secrets.Encryptor` (`internal/database/repositories/git_source.go`)
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

- Global rate limit: **30 requests/second** with a burst of **60**, per client IP
- Applied to all routes via middleware
- Uses a token bucket algorithm

### Client IP

The client IP keys the rate limiter, the login lockout, and audit rows (`internal/web/httputil/clientip.go`). Proxy headers are read only when the direct peer is in `TRUSTED_PROXIES` (default: loopback and private ranges). From a trusted peer, GoForge uses, in order:

1. `CF-Connecting-IP`, when it is a valid IP, **only if `CLOUDFLARE_PROXY_MODE=true`**. With the default (`false`) this header is ignored, even from a trusted peer.
2. The rightmost `X-Forwarded-For` hop that is not a trusted proxy. The leftmost entries are written by the client and are never used on their own; a hop that is not an IP stops the walk at the last trusted hop.
3. `X-Real-IP`, when it is a valid IP.
4. The peer address.

Cloudflare overwrites `CF-Connecting-IP` at its edge, but Traefik forwards it exactly as the client sent it. If a client can reach Traefik without going through Cloudflare (no Cloudflare at all, or a LAN/host port next to a tunnel), it can choose that header. That is why it is opt-in. Only set `CLOUDFLARE_PROXY_MODE=true` when every request reaches Traefik through Cloudflare. Without it, tunnel users fall back to `X-Forwarded-For`, and cloudflared's address may be the key they share.

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
- Docker Compose does not publish Postgres on the host. Other containers reach it on the Compose network

### Recommendations

- Keep Postgres off the host network. Do not add a `5432:5432` mapping
- Use strong, unique database credentials (the example `.env` password is `changeme` for a local copy only)
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
- [ ] Turn on nightly backups (see [Backup & Restore](deployment/backup.md)) and keep `ENCRYPTION_KEY` off the server
- [ ] Review and restrict Docker socket access
- [ ] Configure firewall rules to limit access (allow outbound to Cloudflare only)
- [ ] Set up log monitoring and alerting for security events
