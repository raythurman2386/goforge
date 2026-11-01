# MCP Server

GoForge serves its [Model Context Protocol](https://modelcontextprotocol.io)
tools at **`/mcp`** on the GoForge server itself. An agent uses them to read
projects, deployments, services, containers, logs, and the audit log, and,
when its token allows, to run gated changes.

- **Transport:** streamable HTTP, stateless, JSON responses. `POST /mcp`
  only; `GET` and `DELETE` answer `405` (there is no session and no server
  push).
- **Auth:** a Profile API token in `Authorization: Bearer <token>`. Session
  cookies are ignored. Full-scope tokens are refused.
- **Same checks as the API:** each tool call is one request to the
  [API v1](../reference/api-v1.md) handler, in process, with your token, so
  scope, ownership, protected services, `dry_run`, `approval_ref`, and the
  audit-before-act rule are the ones `/api/v1` enforces.
- There is no tool that reveals a secret.

`cmd/goforge-mcp` is the same tool set as a stdio binary for clients that
cannot speak HTTP; it calls `/api/v1` over HTTPS (see
[stdio fallback](#stdio-fallback)).

## 1. Create an MCP token

Sign in and open **Settings → Profile → MCP → Generate MCP token**. The
preset checks `read` and `write` and expires in 90 days (30, 90, or 365; an
MCP token always expires).

| For | Scopes | Tools |
|-----|--------|-------|
| Read-only agent | `read` | 24 read tools |
| Agent that may deploy, restart, and edit variables | `read`, `write` | 24 read + 14 gated write tools (38) |
| Agent that may also stop, reconfigure, or re-image protected services | `read`, `write`, `protected` | 38 |

The token is shown once and starts with `gf_`. The token list shows a
non-secret hint (`gf_…a1b2`) so you can tell tokens apart.

- **Rotate** gives the same row (name, scopes, expiry) a new secret. The old
  secret stops working at once. The new one is shown once.
- **Revoke** deletes the token.
- Create, rotate, and revoke are written to the audit log (`api_token.create`,
  `api_token.rotate`, `api_token.revoke`) with the name, scopes, and expiry,
  never the token or its hint.

Tokens without scopes (legacy **full** tokens) keep working on the web
routes and `/api/v1`, but `/mcp` answers `403 insufficient_scope`. Create a
scoped MCP token instead.

## 2. Connect a client

The Profile **MCP** card shows the endpoint (`GOFORGE_BASE_URL` + `/mcp`)
and a copy-ready `mcp.json`:

```json
{
  "mcpServers": {
    "goforge": {
      "url": "https://goforge.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${env:GOFORGE_MCP_TOKEN}"
      }
    }
  }
}
```

Set `GOFORGE_MCP_TOKEN` in the environment the client runs in. The token is
never written into `mcp.json`. Clients that do not expand `${env:…}` need
the value some other way; do not commit it.

A raw request, for checking a token:

```bash
curl -sS https://goforge.example.com/mcp \
  -H "Authorization: Bearer $GOFORGE_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

### Answers

| Request | Answer |
|---------|--------|
| No `Authorization: Bearer` (cookie only, Basic, none) | `401`, `WWW-Authenticate: Bearer realm="goforge-mcp"` |
| Unknown, expired, revoked, or rotated-away token | `401`, `WWW-Authenticate: Bearer realm="goforge-mcp", error="invalid_token"` |
| Full token, or a token with only `protected` | `403 insufficient_scope` |
| Browser request from another origin (`Origin` / `Sec-Fetch-Site`) | `403` |
| Write tool with a token that lacks `write` | tool error `insufficient_scope`; nothing runs |

`/mcp` sits beside `/api/v1`: outside the session and CSRF middleware, with
the same 15 s write timeout and the same per-IP rate limiter (one hit per
`/mcp` request; the in-process API calls are not counted again). The audit
log records the `/mcp` caller's IP (proxy headers count only from
`TRUSTED_PROXIES`, as on `/api/v1`) and a `goforge-mcp-http` user agent.

## 3. Protect the shared services

Set the server-side list on the GoForge container (comma-separated service
instance IDs, see [Configuration](../configuration/index.md#api-v1-and-mcp)):

```bash
GOFORGE_PROTECTED_SERVICES=<shared-pg-id>,<shared-redis-id>,<gitea-id>,<vaultwarden-id>,<solidcraft-id>
```

It takes effect when the GoForge container next starts. To protect a service
without a restart, turn on **Protected** on its service page instead; see
[Protected services](services.md#protected-services).

## 4. Turn it off

`/mcp` is on by default. `GOFORGE_MCP_HTTP=false` on the GoForge container
removes the route (it answers `404`) and hides the Profile MCP card.
`/api/v1` and the stdio binary are not affected.

## stdio fallback

`cmd/goforge-mcp` speaks MCP over stdio and calls `/api/v1` with the same
kind of scoped token. Use it only for a client that cannot reach an HTTP MCP
server. Point it at the public HTTPS URL:

```bash
make build-mcp            # bin/goforge-mcp
make docker-build-mcp     # image goforge-mcp:latest (Dockerfile target "mcp")
```

| Variable | Default | Meaning |
|----------|---------|---------|
| `GOFORGE_MCP_URL` | `http://127.0.0.1:8080` (image: `http://goforge:8080`) | GoForge base URL, without `/api/v1`, for example `https://goforge.example.com` |
| `GOFORGE_MCP_TOKEN` | | API token |
| `GOFORGE_MCP_TOKEN_FILE` | | File holding the token, used when `GOFORGE_MCP_TOKEN` is empty |
| `GOFORGE_MCP_ALLOW_WRITES` | off | `true` registers the write tools. Off: read tools only |
| `GOFORGE_MCP_PROTECTED` | empty | Extra protected service instance IDs, merged with the server's list |

```json
{
  "mcpServers": {
    "goforge": {
      "command": "goforge-mcp",
      "env": {
        "GOFORGE_MCP_URL": "https://goforge.example.com",
        "GOFORGE_MCP_TOKEN_FILE": "/home/you/.config/goforge-mcp/token",
        "GOFORGE_MCP_ALLOW_WRITES": "true"
      }
    }
  }
}
```

Keep the token file mode `0600`. Do not pass the token on the command line,
where `ps` shows it. Unlike `/mcp`, the stdio binary needs
`GOFORGE_MCP_ALLOW_WRITES=true` for the write tools, and lists only the tools
it registered.

## Tools

Read tools (always on, read-only):

`health`, `whoami`, `dashboard_stats`, `list_projects`, `get_project`,
`list_environments`, `list_env_vars` (secrets masked), `list_deployments`,
`get_deployment`, `get_deployment_logs`, `list_service_templates`,
`get_service_template`, `list_services`, `get_service` (config masked),
`list_containers`, `get_container`, `get_container_logs`,
`get_container_stats`, `list_audit_log`, `list_git_sources`, `list_repos`,
`list_branches`, `get_backup_status` (admin only; see
[Backups](../deployment/backup.md)), `wait_for`.

`wait_for` follows work a write tool started. Pass the `operation` from a
write result (`kind` is `deployment` with `id` and `project_id`, or
`service` with `id`). It polls once a second for up to `timeout_seconds`
(default 10, at most 12, so it fits the request timeout) and returns
`status`, `terminal`, `timed_out`, `error_message`, and the last
`log_tail` log lines (default 20, at most 200). Call it again while
`timed_out` is true.

Write tools (on `/mcp`: listed when the token has `write`; stdio: only with
`GOFORGE_MCP_ALLOW_WRITES=true` and a `write` token):

`trigger_deployment`, `cancel_deployment`, `rollback_deployment`,
`set_env_var`, `delete_env_var`, `sync_env_vars`, `link_service`,
`unlink_service`, `deploy_service`, `restart_service`,
`update_service_image`, `update_service_config`, `restart_container`,
`stop_container`.

Every write tool takes `dry_run` (default `true`) and `approval_ref`:

1. Call it without `dry_run`. It returns the plan and changes nothing.
2. Get a human approval.
3. Call it again with `dry_run: false` and `approval_ref` set to the approval's
   id. GoForge records the `approval_ref` in `audit_logs` before it acts.

A retry with the same `approval_ref` on the same action and target does not
run the change again. It returns the first run's result and `audit_id` with
`"replayed": true`, the first run's error, or `in_progress` while the first
run is still going. A new attempt needs a new `approval_ref`.

A change that takes longer than the request may wait (about 12 seconds)
keeps running. The tool returns the error code `timeout` with the
`audit_id`; the outcome is written to the audit log as
`api_v1.<action>.completed` or `api_v1.<action>.failed`. Queued work
(`trigger_deployment`, `deploy_service`, `update_service_image`,
`update_service_config`) returns an `operation` to pass to `wait_for`.

### Results

A success returns the API's JSON as text and, when it is a JSON object, as
`structuredContent` too. An error (`isError` true) returns a readable
message as text and `structuredContent`
`{"status", "code", "message", "audit_id"}`: `status` is the HTTP status
(0 when the API was not reached), and `code` is the API error code, for
example `protected_resource`, `writes_disabled`, `bad_request`,
`timeout`, `in_progress`, or `api_unreachable`.

`link_service` returns `added_keys`, `failed_keys`, and `partial`: a key
that could not be written is listed in `failed_keys` instead of failing the
whole call.

`stop_container`, `update_service_config`, and `update_service_image` on a
protected service are refused unless the token has the `protected` scope. The
MCP tools check the protected list first; the API checks again and answers
`403 protected_resource` before any Docker call. `restart_service` and
`restart_container` are allowed on protected services.

## Not covered yet

No OAuth (Profile tokens only), no per-token rate limit, and no Cloudflare
Access integration. Later versions may let the MCP own Postgres tenants on the shared Postgres,
Redis ACL users on the shared Redis (today: `scripts/shared-data/pg-tenant.sh`
and `redis-tenant.sh` on the host), and backups. v1 has none of these. It
also has no project, service, or volume delete, no prune, and no secret-reveal
tool. The only deleting tools are `delete_env_var` and `unlink_service`, and
they are gated writes like the other 12.
