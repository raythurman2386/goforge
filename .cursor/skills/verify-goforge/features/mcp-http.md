# GoForge MCP over HTTP

`goforge serve` answers MCP at `POST /mcp`, beside `/api/v1`. A client such as Cursor sends a Profile API token as `Authorization: Bearer`. A read token sees 23 read tools, and a token with `write` sees 37. Write tools are dry runs by default, need `approval_ref` to run, and the audit row records the caller's IP. Legacy `full` tokens, cookies, and cross-site browser requests are refused. Settings → Profile has an MCP card with the endpoint, an `mcp.json` snippet that reads the token from `${env:GOFORGE_MCP_TOKEN}`, and a Generate MCP token button. Token rows show a `gf_…` hint and can be rotated or revoked, and each of those writes an audit row.

## Sub-features

- `mcp-profile-card`: Settings → Profile shows `id="mcp-card"`, the endpoint `<GOFORGE_BASE_URL>/mcp`, and a snippet holding `${env:GOFORGE_MCP_TOKEN}`.
- `token-mcp-preset`: `GET /settings/tokens/new?preset=mcp` is the preset form (hidden `preset=mcp`, 90 days, no Never). Posting it with no scopes makes a `read,write` token that expires in 90 days (`201`). A preset with only `protected` is `400`.
- `token-hint`: the token list (`GET /settings/tokens`) shows `gf_…` plus the last four characters, never the token. `api_tokens.token_hint` holds the same string.
- `mcp-no-bearer-401`: no `Authorization` is `401` with `WWW-Authenticate: Bearer realm="goforge-mcp"`.
- `mcp-cookie-only-401`: a signed-in session cookie with no bearer is still `401`.
- `mcp-bad-bearer-401`: an unknown, expired, or revoked token is `401` with `error="invalid_token"`.
- `mcp-full-refused`: a legacy `full` token is `403`, JSON `error.code` `insufficient_scope`, and `WWW-Authenticate` holding `error="insufficient_scope"`.
- `mcp-get-405`: `GET /mcp` is `405`. The endpoint is stateless and has no server-sent stream.
- `mcp-initialize`: `initialize` is `200` `application/json` with `serverInfo.name` `goforge`. No session id is needed for later calls.
- `mcp-read-inventory`: `tools/list` with a read token has 23 tools, each `readOnlyHint` true.
- `mcp-write-inventory`: with a `write` token it has 37. The 14 write tools each take `dry_run` and `approval_ref`.
- `mcp-read-call`: `whoami` answers with the caller's own token (`token.scopes` `read`, or `read,write`).
- `mcp-read-token-write-refused`: a read token calling `set_env_var` gets a tool result with `isError` true and text starting `insufficient_scope`. Nothing reaches `/api/v1`.
- `mcp-dry-run`: `set_env_var` without `dry_run` returns `"dry_run": true` and writes no `env_variables` row.
- `mcp-approval-audit`: `set_env_var` with `dry_run:false` and an `approval_ref` writes the row. The `audit_logs` row is `api_v1.set_env_var` with that `approval_ref`, the client IP from `X-Forwarded-For` (the peer is loopback, so trusted), and user agent `goforge-mcp-http …`.
- `mcp-protected-refusal`: on a service whose id is in `GOFORGE_PROTECTED_SERVICES`, `update_service_config` and `update_service_image` with a `write` token return `isError` true naming `protected`. The same `PATCH /api/v1/services/{id}/config` is `403`, and the row is unchanged.
- `mcp-protected-restart-allowed`: `restart_service` on that service returns a dry-run plan, not a refusal.
- `mcp-cross-origin-403`: `POST /mcp` with `Origin: https://evil.example.test` and `Sec-Fetch-Site: cross-site` is `403`, even with a valid token.
- `token-rotate`: `POST /settings/tokens/{id}/rotate` (`200`, modal `API Token Rotated`) gives a new token on the same row. The old token is `401` at once, the new one works, and the hint changes.
- `token-revoke`: `DELETE /settings/tokens/{id}` is `200`, and the token is `401` on `/mcp` after that.
- `token-audit`: `audit_logs` has `api_token.create`, `api_token.rotate`, and `api_token.revoke` rows, and no token value appears in any `details`.

Covered by unit tests, not by the drive: `GOFORGE_MCP_HTTP=false` leaves `/mcp` unmounted (`internal/config`, `internal/web` `TestRouter_MCPOutsideCSRFAndSession`). The 15 s timeout and the shared rate limiter come from the router group. The in-process hop ignoring the outer chi route is `TestInProcessIgnoresOuterRoute`.

## How to get to it (user POV)

- Open Settings → Profile. The MCP card shows the endpoint and the `mcp.json` snippet.
- Press Generate MCP token, keep read and write (90 days), and copy the token from the modal. It is shown once.
- Export it as `GOFORGE_MCP_TOKEN` where Cursor runs, and paste the snippet into `~/.cursor/mcp.json`.
- Rotate or Revoke from the token row.

## Driving it with verify.sh

`verify.sh drive-mcp-http` runs after `drive-register` and `drive-login` (it uses project `verify-smoke` and its first environment). It creates tokens `verify-mcp-write`, `verify-mcp-read`, and `verify-legacy-full` through the Profile form, checks every sub-feature above, and writes `$EVIDENCE/mcp-http-verdict.txt`. Tokens go only to mode `600` files in `$STATE_DIR`, `mcp-<name>.token` and `mcp-<name>.hdr`. curl reads the header file with `-H @file`, so no token is on argv. The drive deletes both files before it exits, and it fails with exit `2` if any token value shows up in the verdict.

The drive needs `GOFORGE_PROTECTED_SERVICES` set to one UUID on the stack. It reads that id from `whoami` and inserts a stopped `service_instances` row with it, owned by the admin. Nothing is deployed and no container is touched.

Host mode, used when `launch` refuses because port 8080 is taken (build the branch, use a throwaway Postgres, never a live one):

```bash
sudo -n docker run -d --name gfmcp-test-pg -e POSTGRES_USER=goforge -e POSTGRES_PASSWORD=goforge \
  -e POSTGRES_DB=goforge -p 127.0.0.1:55433:5432 postgres:17-alpine
sudo -n docker exec gfmcp-test-pg psql -U goforge -c "create database goforge_verify"
make templ css && go build -o /tmp/gfmcp-goforge ./cmd/goforge
# serve from a mode 600 env file: GOFORGE_PORT=18080, GOFORGE_HOST=127.0.0.1,
# GOFORGE_BASE_URL=http://127.0.0.1:18080, DATABASE_URL at goforge_verify on 55433,
# random ENCRYPTION_KEY/SESSION_SECRET/CSRF_SECRET/WEBHOOK_SECRET (openssl rand -base64 32),
# GOFORGE_ENABLE_REGISTRATION=true, GOFORGE_ADMIN_EMAIL=verify-admin@example.test,
# GOFORGE_PROTECTED_SERVICES=$(cat /proc/sys/kernel/random/uuid), LOG_FORMAT=json;
# stdout to /tmp/goforge-verify/host-app.log
install -d -m 700 /tmp/goforge-verify/state
export GOFORGE_VERIFY_BASE=http://127.0.0.1:18080 GOFORGE_VERIFY_ENABLE_REGISTRATION=true \
  GOFORGE_VERIFY_DOCKER="sudo -n docker" \
  GOFORGE_VERIFY_PSQL="sudo -n docker exec -i gfmcp-test-pg psql -U goforge -d goforge_verify -v ON_ERROR_STOP=1 -tA" \
  GOFORGE_VERIFY_APP_LOGS="cat /tmp/goforge-verify/host-app.log"
verify.sh drive-register && verify.sh drive-login && verify.sh drive-mcp-http
```

Stop the host serve by its PID, then `sudo -n docker rm -f -v gfmcp-test-pg`. With registration open, the `register-closed*` and `register-admin-role` lines in `register-verdict.txt` read `fail`. That is expected in host mode.

## Gotchas

- `drive-register` needs `$STATE_DIR` to exist. `launch` makes it. In host mode, run `install -d -m 700 /tmp/goforge-verify/state` first.
- Re-running the drive on the same database adds more token rows, and `token-audit` counts rows by token name. Start from a fresh database, or drop and recreate `goforge_verify`.
- Token creation answers `201`, not `200`.
- `whoami` nests scopes under `token.scopes`. `protected_services` is top level.
- The endpoint is stateless. Every POST is answered on its own, so `tools/list` or `tools/call` works without a prior `initialize` on the same connection.
- The DNS-rebinding check in the SDK refuses a request to a loopback listener whose `Host` is not loopback. Drive `127.0.0.1:18080`, not a LAN name, in host mode.
- Do not drive `127.0.0.1:8080` or the public host on minis. Those are the live stack.
