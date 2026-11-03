# Authentication Configuration

GoForge supports two authentication methods: local email/password and GitHub OAuth.

## Local Authentication

Local authentication works out of the box with no additional configuration. Users register with an email and password, which is hashed using Argon2id before storage.

`GOFORGE_ENABLE_REGISTRATION` defaults to false. When no users exist, the register form and a new GitHub login can still create the first account. That account is a normal user. After that account exists, both paths stay closed unless the variable is true. Set `GOFORGE_ADMIN_EMAIL` to an account's address and restart GoForge to grant the admin role. The same address is also admin if it registers while the variable is already set. Matching ignores case: `Admin@Example.com` and `admin@example.com` are one account. Addresses are stored in lowercase, and a second registration that differs only by case is rejected. Linking GitHub to an existing email does not change the role.

### Two-factor authentication (TOTP)

A local password account can turn on two-factor authentication in **Settings → Profile → Two-factor authentication**. Nothing to configure; it needs `ENCRYPTION_KEY`, which seals the TOTP key.

1. Enter the current password and choose **Set up 2FA**.
2. Scan the QR code with an authenticator app (or type the setup key), then enter the 6-digit code it shows.
3. Save the 10 recovery codes. They are shown once, and each works once.

From then on, sign-in asks for the password and then a code from the app or a recovery code at `/auth/2fa` (5 minutes, 5 tries). Wrong codes count toward the login lockout. To turn it off, enter the password and a code. If a user loses the app and the recovery codes, run `goforge user reset-2fa <email>` on the host (`docker compose exec goforge ./goforge user reset-2fa <email>` with Docker Compose).

Sign in with GitHub does not ask for this code; it relies on the GitHub account's 2FA. GitHub sign-in links a local account with the same email, so turn on 2FA at GitHub as well. API tokens and `/mcp` never ask for a code. Details: [Security → Two-factor authentication](../security.md#two-factor-authentication).

### Password Hashing

GoForge uses Argon2id with these parameters:

| Parameter | Value |
|-----------|-------|
| Algorithm | Argon2id |
| Memory | 64 MB |
| Iterations | 1 |
| Parallelism | 4 |
| Salt Length | 16 bytes |
| Key Length | 32 bytes |

These meet OWASP's minimum recommendations for Argon2id.

## GitHub OAuth

GitHub OAuth enables users to log in with their GitHub account and connect repositories for deployment.

GitHub sign-in relies on GitHub's own two-factor authentication. GoForge does not ask for its TOTP code after a GitHub login, even when the linked account has 2FA on.

### Step 1: Create a GitHub OAuth App

1. Go to [GitHub Developer Settings](https://github.com/settings/developers)
2. Click **New OAuth App**
3. Fill in the details:
   - **Application name:** GoForge (or your preferred name)
   - **Homepage URL:** Your GoForge base URL (e.g., `https://goforge.example.com`)
   - **Authorization callback URL:** `{GOFORGE_BASE_URL}/auth/github/callback`

4. Click **Register application**
5. Note the **Client ID** and generate a **Client Secret**

### Step 2: Configure Environment

```bash
GITHUB_CLIENT_ID=your_client_id_here
GITHUB_CLIENT_SECRET=your_client_secret_here
GOFORGE_BASE_URL=https://goforge.example.com
```

!!! important "Callback URL must match"
    The callback URL in your GitHub OAuth App settings must exactly match `{GOFORGE_BASE_URL}/auth/github/callback`. If using a different port or domain, update both locations.

### OAuth Scopes

GoForge requests these GitHub OAuth scopes:

| Scope | Purpose |
|-------|---------|
| `user:email` | Read the user's email address for account creation |
| `repo` | Access repositories for deployment (read code, set up webhooks) |

### Token Storage

GitHub access tokens are encrypted at rest using AES-256-GCM before being stored in the database. The `ENCRYPTION_KEY` environment variable provides the encryption key.

## Session Management

Sessions are cookie-based with the following security properties:

| Property | Value |
|----------|-------|
| Token Length | 32 bytes (random) |
| Token Storage | SHA-256 hashed (database never stores raw tokens) |
| Cookie Name | `goforge_session` |
| Cookie Flags | `HttpOnly`, `SameSite=Lax`, `Secure` (production) |
| Session Duration | 24 hours (30 days with "Remember Me") |
| Idle Timeout | Activity tracked with 1-minute debounce |

### Session Lifecycle

1. On login, a random 32-byte token is generated
2. The token is SHA-256 hashed and stored in the database
3. The plain token is set as a cookie in the browser
4. On each request, the middleware hashes the cookie value and looks up the session
5. An expired session is deleted when it is next used, and `goforge serve` also deletes all expired session rows at startup and then hourly

## CSRF Protection

CSRF tokens are HMAC-SHA256 values keyed by `CSRF_SECRET` (`internal/auth/csrf.go`). A planted cookie does not pass:

1. Signed in, the token is `HMAC(CSRF_SECRET, SHA-256(session token))`, bound to that session
2. Signed out (login and register forms), the token is a random nonce plus its HMAC, stored in the `goforge_csrf` cookie
3. HTMX sends the token in the `X-CSRF-Token` header on state-changing requests (POST, PUT, DELETE, PATCH); plain forms send the `csrf_token` field
4. The middleware checks the MAC in constant time and answers `403` when it does not verify

Details: [Security → CSRF Protection](../security.md#csrf-protection).

### Configuring HTMX

HTMX is pre-configured in the base template to send the CSRF token automatically. htmx 4 inherits an attribute only when it says so, hence `:inherited`:

```html
<body hx-headers:inherited='{"X-CSRF-Token": "{{csrfToken}}"}'>
```

## Rate Limiting

GoForge applies a global rate limiter to all routes:

- Token bucket algorithm (30 requests/second, burst of 60) per client IP
- Applied globally via middleware (`internal/web/router.go`)
- Not specific to authentication endpoints -- covers all HTTP routes
- The client IP comes from `TRUSTED_PROXIES` peers only (rightmost untrusted `X-Forwarded-For` hop; `CF-Connecting-IP` first when `CLOUDFLARE_PROXY_MODE=true`). See [Security → Client IP](../security.md#client-ip)

`POST /auth/login` also has a lockout: 10 failed logins per client IP, or per account from any IP, within 15 minutes lock that key for 15 minutes and the login answers `429` with `Retry-After`.
