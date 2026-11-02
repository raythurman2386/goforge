# Security

> **⚠️ Warning: GoForge is in active development.** 
> This document describes the current security model. The items under "Known limitations" should be addressed before production use. See the [Security Policy](https://github.com/raythurman2386/goforge/blob/main/SECURITY.md) for full disclaimers.

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

The OAuth flow validates the `state` parameter using a cookie-based approach: a UUID is generated, stored in a `github_oauth_state` cookie (HttpOnly, 5-minute expiry), and verified on callback (`GitHubLogin` and `GitHubCallback` in `internal/web/handlers/auth.go`).

---

## Session Management

- Sessions use cryptographically random tokens (32 bytes, base64url-encoded)
- Session tokens are **hashed with SHA-256** before storage in the database
- Only the hash is stored; the raw token exists only in the client cookie
- Session cookies use:
    - `HttpOnly: true` - Not accessible to JavaScript
    - `SameSite: Lax` - Prevents cross-site request forgery for most cases
    - `Secure` on HTTPS requests (`X-Forwarded-Proto: https` or TLS)
    - `Path: /`
- Sessions last 24 hours, or 30 days with "remember me" (GitHub login sessions last 30 days)
- Expired sessions are deleted on access, and `goforge serve` deletes every expired session row at startup and then hourly

### Known Limitations

- Each login, registration, and GitHub callback creates a new session with a new random token. There is no anonymous session that becomes authenticated, so a session cookie planted before login is not promoted
- **Password change keeps other sessions**: changing the password does not end the account's other sessions
- **No concurrent session limits**: Users can have unlimited active sessions

---

## CSRF Protection

State-changing requests (`POST`, `PUT`, `DELETE`, `PATCH`) in the UI group must send a CSRF token in the `X-CSRF-Token` header (HTMX, via `hx-headers`) or the `csrf_token` form field. Tokens are HMAC-SHA256 values keyed by `CSRF_SECRET` (`internal/auth/csrf.go`):

1. **Signed in**: the token is `HMAC(CSRF_SECRET, SHA-256(session token))`. It is stable for the session, fails in any other session, and cannot be computed without both the session cookie and the secret.
2. **Signed out** (login and register forms): the token is a random nonce plus `HMAC(CSRF_SECRET, nonce)`, stored in the `goforge_csrf` cookie and echoed in the form.
3. The token is compared in constant time. A `goforge_csrf` cookie without a valid MAC, for example one planted by an app on a sibling subdomain, is replaced and the request gets `403 invalid CSRF token`.

The `goforge_csrf` cookie is not HttpOnly, is `SameSite=Lax`, and is `Secure` on HTTPS requests. Rotating `CSRF_SECRET` invalidates tokens in open pages; reload to get a new one.

**Exemptions**: Webhook routes (`/webhooks/*`), `/api/v1/*`, and `/mcp` (bearer only, cookies ignored) are outside the CSRF group. `/mcp` also runs `http.CrossOriginProtection`, so a browser on another origin gets `403` even with a token.

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

The CSP does not restrict scripts or styles because a few pages still use inline `<script>` blocks (service detail, container detail, the Git repository browser) and HTMX attributes; the UI kit itself has no inline event handlers since #89. The CSP only blocks framing (clickjacking), `<base>` rewrites, and plugins. HSTS leaves out `includeSubDomains` because deployed apps live on sibling subdomains GoForge does not control.

---

## Authorization

- Route-level authorization is enforced by the `RequireAuth` middleware
- Users can only access their own resources (projects, environments, services)
- Resource ownership is validated at the repository layer via `user_id` filtering

Accounts have a `role` of `admin` or `user`. Registration creates users, including the first account. `GOFORGE_ADMIN_EMAIL` promotes that address on process start and when the account is created. The match is case-insensitive, and account emails are stored in lowercase. Host actions such as Docker prune require the admin role and return 403 for anyone else. Project and service ownership is still checked separately, so a user only sees their own resources. Deployment get, rollback, cancel, and status also require the deployment's environment to belong to the project in the path; a cross-project id is not found.

### API tokens and MCP

- Profile API tokens are 32 random bytes. Tokens created since migration `021_api_token_hint` are `gf_` plus 64 hex characters, so secret scanners can spot a leaked one. Only the SHA-256 hash is stored, plus a non-secret hint (`gf_…` and the last 4 characters) for the token list.
- **Rotate** replaces the hash and hint on the same row; the old secret stops working on its next request. **Revoke** deletes the row. Create, rotate, and revoke are audited (`api_token.create`, `api_token.rotate`, `api_token.revoke`) with name, scopes, and expiry only.
- `/mcp` (streamable HTTP MCP) accepts only `Authorization: Bearer` with a scoped token that grants `read`. Legacy `full` tokens and `protected`-only tokens get `403 insufficient_scope`; missing, unknown, expired, or rotated tokens get `401` with `WWW-Authenticate: Bearer realm="goforge-mcp"`. MCP tokens from the Profile preset always expire (30, 90, or 365 days).
- Each MCP tool call is served by the `/api/v1` handler in the same process, with the caller's token, so `/mcp` cannot do anything `/api/v1` would refuse that token: scope, ownership, protected services, `dry_run`, `approval_ref`, and audit-before-act are the same code. Write tools need `write`; a read token gets an `insufficient_scope` tool error and nothing runs. The token is never sent to another service.
- The go-sdk handler also refuses (`403`) a request that arrives on a loopback listener with a non-loopback `Host` (DNS rebinding protection, left on). Traefik reaches GoForge on the Docker network, so routed traffic is not affected; a proxy on the host that forwards to `127.0.0.1:8080` while keeping the public `Host` would be refused.
- `GOFORGE_MCP_HTTP=false` removes `/mcp` entirely.

---

## Secrets & Credential Storage

### Environment Variables

- Environment variables marked as `is_secret` are encrypted at rest using AES-GCM via the `secrets.Encryptor` (`internal/database/repositories/env_variable.go`)
- The `ENCRYPTION_KEY` environment variable provides the key material
- Variables are decrypted at read time and injected into containers at deploy time via Docker environment variables

### Git Source Credentials

- Git provider access tokens and SSH private keys are stored in the `git_sources` table
- These are encrypted at rest using the same `secrets.Encryptor` (`internal/database/repositories/git_source.go`)
- GitHub user access tokens in the `users` table are also encrypted (`internal/database/repositories/user.go`)

### One-click services, backups, and tokens

- One-click service `config` and `connection_info` (generated passwords included) are encrypted with the same key
- The S3 access key ID and secret saved in Settings → Profile → Backups are sealed with `ENCRYPTION_KEY` and never sent back to the browser. `ENCRYPTION_KEY` itself is never uploaded with a backup
- API tokens and session tokens are stored only as SHA-256 hashes
- The Sentry DSN saved in Settings → Profile → Error reporting is sealed with `ENCRYPTION_KEY` and never sent back to the browser (only scheme, host, and project)

### Error reporting (Sentry)

Error reporting is off unless an admin saves a DSN (or `SENTRY_DSN` is set and nothing is saved). When it is on, every event is scrubbed before it leaves the process (`internal/errreport/scrub.go`): request cookies, body, query string, environment, and the `Cookie`, `Set-Cookie`, `Authorization`, `Proxy-Authorization`, `X-CSRF-Token`, `X-API-Key`, `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP`, and `Forwarded` headers are removed; the URL loses its query and fragment; no user is attached (no id, email, name, or IP; only an id an SDK integration set would be kept); messages, exception values, and breadcrumbs go through the notification redactor (connection strings, `password=`, bearer tokens, and known secrets). `SendDefaultPII` is off and tracing is off. A DSN with a secret key, or plain `http://` to a non-loopback host, is refused. The test event uses a separate client, so it changes nothing that is live.

### Recommendations

- GoForge has no `ENCRYPTION_KEY` rotation tooling. Changing the key makes existing encrypted values unreadable
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

### Traefik

- With Cloudflare Tunnel, cloudflared reaches Traefik over the `traefik` Docker network, so Traefik needs no public host port. The repo `docker-compose.yml` still publishes 80 and 443 on all interfaces for direct (LAN or public IP) installs; tunnel deployments should bind them to `127.0.0.1` or drop them in a `docker-compose.override.yml`
- The Traefik API and dashboard are off by default. `TRAEFIK_DASHBOARD_INSECURE=true` serves them without authentication on `127.0.0.1:${TRAEFIK_DASHBOARD_PORT:-8081}` only; no router publishes `api@internal` on the web or websecure entrypoints
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
| GitHub | `/webhooks/github` | HMAC-SHA256 of the body with `WEBHOOK_SECRET` in `X-Hub-Signature-256`, compared with `hmac.Equal` |
| GitLab | `/webhooks/gitlab` | `X-Gitlab-Token` equal to `WEBHOOK_SECRET`, constant-time compare |
| Gitea | `/webhooks/gitea` | HMAC-SHA256 in `X-Gitea-Signature` (falls back to `X-Hub-Signature-256`) |

Bodies are capped at 1 MB. GitHub and Gitea `ping` events are answered before the signature check and trigger nothing. Webhook routes are outside the CSRF group and need no session.

!!! warning "Set `WEBHOOK_SECRET`"
    Outside dev mode GoForge refuses to start while `WEBHOOK_SECRET` is empty or only whitespace. With an empty secret anyone could compute the HMAC (empty key) or send an empty `X-Gitlab-Token`, so a forged push for a project's repository and branch could trigger a deployment. Dev mode generates a secret and keeps it in `GOFORGE_DEV_SECRETS_FILE`. Generate it with `openssl rand -base64 32`.

---

## Docker Security

### Build Process

- Images are built using the Docker SDK (`docker/build.go`)
- Build context is sent from cloned repository contents
- Dockerfiles are user-provided and could contain arbitrary instructions

### Runtime

- Containers run with default Docker security settings
- Resource limits (CPU, memory) are configurable per environment
- The GoForge application itself runs as root in the Docker container (the optional `mcp` stdio image runs as uid 10001)
- GoForge does not mount the Docker socket into the project and service containers it deploys. See "Docker socket trust model" in `SECURITY.md`

### Recommendations

- A non-root `goforge` image is not available yet; the socket mount makes the process root-equivalent on the host either way
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
| Write Timeout | none on the server | SSE streams are long-lived; `/api/v1`, `/mcp`, and the non-streaming UI routes get a 15s `middleware.Timeout` instead |
| Idle Timeout | 60s | |
| Max Header Bytes | 1MB | Go's default (`http.DefaultMaxHeaderBytes`) |
| Rate limit | 30 req/s, burst 60 per client IP | Global middleware |

---

## Security Checklist for Production

### Before Production Use

> ⚠️ **Important**: GoForge is in active development. Review and address the following before production deployment:

- [ ] Understand that the project has not been security-audited
- [ ] Test thoroughly in a non-production environment
- [ ] Review all known limitations in this document

### Deployment Checklist

- [ ] Set strong, unique values for `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, and `WEBHOOK_SECRET`
- [ ] Use a strong `DATABASE_URL` password (not the default)
- [ ] Configure `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` if using OAuth
- [ ] **Use Cloudflare Tunnel** for external access (recommended)
- [ ] Enable HTTPS via Traefik with valid SSL certificates
- [ ] Keep PostgreSQL unpublished (the repo `docker-compose.yml` does not publish it; do not add a mapping in an override)
- [ ] Set `TRUSTED_PROXIES` to your proxy addresses, and `CLOUDFLARE_PROXY_MODE=true` only when every request arrives through Cloudflare
- [ ] Turn on nightly backups (Settings → Profile → Backups; see [Backup & Restore](deployment/backup.md)) and keep a copy of `ENCRYPTION_KEY` somewhere other than the server
- [ ] Review and restrict Docker socket access. The read-write socket in the `goforge` container, Traefik's `:ro` socket, and `docker` group membership are all root-equivalent on the host (see `SECURITY.md`, "Docker socket trust model")
- [ ] Keep the Traefik dashboard off (`TRAEFIK_DASHBOARD_INSECURE=false`, the default)
- [ ] Configure firewall rules to limit access (allow outbound to Cloudflare only)
- [ ] Set up log monitoring and alerting for security events
