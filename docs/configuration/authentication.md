# Authentication Configuration

GoForge supports two authentication methods: local email/password and GitHub OAuth.

## Local Authentication

Local authentication works out of the box with no additional configuration. Users register with an email and password, which is hashed using Argon2id before storage.

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
5. Expired sessions are cleaned up on access

## CSRF Protection

GoForge uses the double-submit cookie pattern for CSRF protection:

1. A random CSRF token is generated and stored in a cookie (`goforge_csrf`)
2. The cookie has `HttpOnly: false` so HTMX can read it
3. HTMX sends the token in the `X-CSRF-Token` header on state-changing requests (POST, PUT, DELETE, PATCH)
4. The server middleware validates that the header matches the cookie

### Configuring HTMX

HTMX is pre-configured in the base template to send the CSRF token automatically:

```html
<body hx-headers='{"X-CSRF-Token": "{{csrfToken}}"}'>
```

## Rate Limiting

GoForge applies a global rate limiter to all routes:

- Token bucket algorithm (30 requests/second, burst of 60)
- Applied globally via middleware (`internal/web/router.go:38`)
- Not specific to authentication endpoints -- covers all HTTP routes
