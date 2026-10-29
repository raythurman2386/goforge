# API v1 Reference

`/api/v1` is a JSON API for automation. The GoForge MCP server
(`cmd/goforge-mcp`, see [MCP server](../guides/mcp.md)) is its main client.

- **Bearer only.** Send `Authorization: Bearer <token>`. Session cookies are
  ignored, so the API is outside the CSRF group and a browser session cannot
  call it.
- **Scoped tokens.** Every route needs a scope (below).
- **Gated writes.** Every mutation is a dry run unless it says
  `"dry_run": false`, and a real run needs an `approval_ref` that is written to
  `audit_logs` before anything changes.
- **No secrets out.** Secret env vars and secret service config are masked.
  There is no route that reveals a secret, a token, or git credentials.

Route definitions: `internal/apiv1/handler.go`.

## Tokens and scopes

Create a token in **Settings → Profile → API Tokens → Generate Token**. Pick a
name, an expiry, and the scopes. The raw token is shown once.

| Scope | Grants |
|-------|--------|
| `read` | Every `GET` route |
| `write` | `read` plus the gated mutations below |
| `protected` | Stop, config edit, and image update of a protected service. Combine it with `write`; on its own it grants nothing else |
| `full` | Legacy. Tokens created before scopes existed, or with no box checked, are `full`: `read` and `write` on `/api/v1`, never `protected`, and the only kind the UI routes accept |

A scoped token (`read`, `write`, `protected`) only works on `/api/v1`; the UI
routes answer it `401`. Migration `018_api_token_scopes` adds
`api_tokens.scopes` and sets every existing token to `{full}`.

## Errors

Errors are JSON with a stable code:

```json
{"error": {"code": "protected_resource", "message": "service … is protected; stop_container needs a token with the protected scope"}}
```

| Status | Code | When |
|--------|------|------|
| 400 | `bad_request` | Bad JSON, unknown field, bad key or value |
| 400 | `approval_ref_required` | `dry_run` is `false` and `approval_ref` is empty, longer than 200 characters, or has control characters |
| 401 | `unauthorized` | No bearer token, or an unknown or expired one |
| 403 | `insufficient_scope` | The token lacks the route's scope |
| 403 | `forbidden` | Another user's service instance, container, or git source |
| 403 | `protected_resource` | Stop, config edit, or image update of a protected service without the `protected` scope |
| 404 | `not_found` | Missing route, or a missing or another user's project, environment, or deployment |
| 409 | `conflict` | The deployment or service is busy or in the wrong state |
| 422 | `immutable_field` | A config edit changes an immutable field |
| 500 | `internal_error` | Includes a failed audit insert; nothing was changed |
| 503 | `unavailable` | A dependency the route needs is not configured |

Ownership answers match the UI routes (#28): another user's project,
environment, or deployment is `404`; another user's service instance or
container is `403`.

## Gated mutations

Every mutation body accepts:

| Field | Default | Meaning |
|-------|---------|---------|
| `dry_run` | `true` | `true` returns the plan and changes nothing. No approval needed |
| `approval_ref` | empty | Required when `dry_run` is `false`: the id of the human approval, for example a Tater message id |

Order of checks on every mutation:

1. Bearer token, then the `write` scope.
2. Ownership of the target (`404` or `403`).
3. Protected refusal (`403 protected_resource`), before any Docker call. This
   applies to dry runs too.
4. Dry run: answer the plan.
5. `approval_ref` check (`400 approval_ref_required`).
6. Insert `audit_logs` row `api_v1.<action>` with `details` =
   `{via, approval_ref, token_id, token_name, target, changes}`. If the insert
   fails the API answers `500` and does nothing.
7. Run the change. If it fails, a best-effort `api_v1.<action>.failed` row is
   added with the same `approval_ref`.

`audit_logs.resource_id` is a UUID column, so a Docker container id is stored
in `details.resource_ref` instead.

A mutation answers:

```json
{
  "action": "restart_service",
  "dry_run": false,
  "target": {"service_instance_id": "…", "service": "db", "protected": true},
  "plan": "restart the container of db in place (no recreate, config and volumes unchanged)",
  "approval_ref": "t1250u",
  "audit_id": "…",
  "result": {"service_instance_id": "…", "status": "restarted"}
}
```

`target` and `changes` never hold a secret value.

## Protected services

`GOFORGE_PROTECTED_SERVICES` (comma-separated service instance IDs) is the
server-side list. `GET /services` and `GET /services/{id}` show
`"protected": true`, and `GET /whoami` returns the list.

| Action | Needs |
|--------|-------|
| `POST /containers/{id}/stop` of a protected service's container | `write` + `protected` |
| `PATCH /services/{id}/config` | `write` + `protected` |
| `POST /services/{id}/image` | `write` + `protected` |
| `POST /services/{id}/restart`, `POST /containers/{id}/restart` | `write` only |

## Read routes (`read`)

| Method | Path | Returns |
|--------|------|---------|
| `GET` | `/api/v1/health` | Status, version, database and Docker health |
| `GET` | `/api/v1/whoami` | User, token scopes, grants, protected list |
| `GET` | `/api/v1/dashboard/stats` | Your project, service, and container totals |
| `GET` | `/api/v1/projects` | Your projects |
| `GET` | `/api/v1/projects/{id}` | One project with environments and linked services |
| `GET` | `/api/v1/projects/{id}/environments` | Its environments |
| `GET` | `/api/v1/projects/{id}/environments/{envId}/variables` | Variables; secret values are `********` |
| `GET` | `/api/v1/projects/{id}/deployments?env_id=&limit=` | Deployments, newest first (limit 20, max 500) |
| `GET` | `/api/v1/projects/{id}/deployments/{deploymentId}` | One deployment |
| `GET` | `/api/v1/projects/{id}/deployments/{deploymentId}/logs?tail=` | Last log lines (200, max 5000) |
| `GET` | `/api/v1/service-templates` | Templates |
| `GET` | `/api/v1/service-templates/{name}` | One template; secret defaults are omitted |
| `GET` | `/api/v1/services` | Your service instances with a `protected` flag |
| `GET` | `/api/v1/services/{id}` | Masked config and connection info, immutable fields, named volumes, container state |
| `GET` | `/api/v1/containers` | Your tracked containers |
| `GET` | `/api/v1/containers/{id}` | State, ports, mounts, limits, networks. No env, no command, labels limited to Traefik routing |
| `GET` | `/api/v1/containers/{id}/logs?tail=&since=` | Last log lines (200, max 5000, 2 MB). Known service secrets are redacted |
| `GET` | `/api/v1/containers/{id}/stats` | One CPU, memory, network sample |
| `GET` | `/api/v1/audit-log?limit=&offset=&resource_type=&resource_id=&action=&all_users=` | Your audit rows (limit 50, max 500). `all_users=true` works for admins only |
| `GET` | `/api/v1/git-sources` | Your git sources (`has_token` only, never the token) |
| `GET` | `/api/v1/git-sources/{id}/repos` | Repositories the source can see |
| `GET` | `/api/v1/git-sources/{id}/branches?repo=owner/name` | Branches |

## Mutation routes (`write`)

| Method | Path | Body (plus `dry_run`, `approval_ref`) | Notes |
|--------|------|------|-------|
| `POST` | `/api/v1/projects/{id}/deployments` | `env_id` (optional) | Manual deploy of the environment's branch |
| `POST` | `/api/v1/projects/{id}/deployments/{deploymentId}/cancel` | | `409` if already finished |
| `POST` | `/api/v1/projects/{id}/deployments/{deploymentId}/rollback` | `reason` | Rolls the deployment's environment back |
| `POST` | `/api/v1/projects/{id}/environments/{envId}/variables` | `key`, `value`, `is_secret` | Creates or updates by key. The value is write-only: never echoed or audited |
| `POST` | `/api/v1/projects/{id}/environments/{envId}/variables/sync` | | Adds keys from the repo's `.env.example` |
| `DELETE` | `/api/v1/projects/{id}/environments/{envId}/variables/{key}` | | |
| `POST` | `/api/v1/projects/{id}/services` | `service_instance_id` | Links and injects connection variables; the plan lists keys added or skipped |
| `DELETE` | `/api/v1/projects/{id}/services/{serviceId}` | | Unlinks; the service keeps running |
| `POST` | `/api/v1/services` | `template`, `name`, `domain`, `config` | Unknown config fields are `400`. Secrets show as `(write-only)` or `(generated)` in the plan |
| `POST` | `/api/v1/services/{id}/restart` | | Restart in place. Allowed on protected services |
| `POST` | `/api/v1/services/{id}/image` | | Pull and recreate; named volumes kept. Protected-gated |
| `PATCH` | `/api/v1/services/{id}/config` | `config` | Returns a masked diff. Immutable change is `422`; a blank password keeps the stored one; no change is a `200` no-op. Protected-gated |
| `POST` | `/api/v1/containers/{id}/stop` | | Protected-gated through the container's service |
| `POST` | `/api/v1/containers/{id}/restart` | | Allowed on protected services |

## Not in v1

On purpose: deleting projects or services, prune, revealing secrets, token,
git-source, password, or profile changes, migrations, Postgres tenants, Redis
ACLs, volumes, and compose operations. Rate limiting is per client IP, not per
token.

## Example

```bash
# Plan (dry run is the default)
curl -s -X POST -H "Authorization: Bearer $GOFORGE_TOKEN" \
  http://127.0.0.1:8080/api/v1/services/$SERVICE_ID/restart

# Run it with an approval reference
curl -s -X POST -H "Authorization: Bearer $GOFORGE_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"dry_run": false, "approval_ref": "t1250u"}' \
  http://127.0.0.1:8080/api/v1/services/$SERVICE_ID/restart
```
