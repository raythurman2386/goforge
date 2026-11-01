# API v1 Reference

`/api/v1` is a JSON API for automation. The GoForge MCP tools (served at
`/mcp`, and by the `cmd/goforge-mcp` stdio fallback; see
[MCP server](../guides/mcp.md)) are its main client: every MCP tool call is
one `/api/v1` request with the caller's token.

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

Create a token in **Settings → Profile → API Tokens → Generate Token** (or
**MCP → Generate MCP token** for the MCP preset: `read` + `write`, 90 days).
Pick a name, an expiry, and the scopes. The raw token is shown once. Tokens
created since migration `021_api_token_hint` start with `gf_` and store a
non-secret hint (`gf_…a1b2`) shown in the token list; older tokens keep
working without a prefix or hint. **Rotate** puts a new secret on the same
row (same id, name, scopes, expiry) and the old secret stops working at once.
Create, rotate, and revoke write `api_token.create`, `api_token.rotate`, and
`api_token.revoke` audit rows without the token or hint.

| Scope | Grants |
|-------|--------|
| `read` | Every `GET` route |
| `write` | `read` plus the gated mutations below |
| `protected` | Stop, config edit, and image update of a protected service. Combine it with `write`; on its own it grants nothing else |
| `full` | Legacy. Tokens created before scopes existed, or with no box checked, are `full`: `read` and `write` on `/api/v1`, never `protected`, and the only kind the UI routes accept |

A scoped token (`read`, `write`, `protected`) only works on `/api/v1` and
`/mcp`; the UI routes answer it `401`. `/mcp` refuses `full` tokens (`403
insufficient_scope`) and tokens with only `protected`. Migration `018_api_token_scopes` adds
`api_tokens.scopes` and sets every existing token to `{full}`.

### Restricting a token to projects and services

The token form also has **Restrict to (optional)**: check any of your
projects and services. With nothing checked the token is host-wide, as
before. With something checked the token only reaches:

- the listed projects, with their environments, variables, deployments,
  deployment logs, and deployment containers;
- the listed services and their containers.

Scopes still decide what kind of action is allowed, and a protected service
still needs `protected`. A restricted token needs explicit scopes (`full` is
refused) and works only on `/api/v1` and `/mcp`. Only your own projects and
services can be picked; the lists are checked again on every request (a
resource that changed owner is `404` or `403`, then the list applies).

A restricted token gets `403 resource_out_of_scope` for anything off its
lists, before any dry run, audit row, or Docker call:

- `GET`, mutations, and logs of an unlisted project, deployment, service, or
  container (another user's resources still answer `404`/`403` first);
- `POST /projects/{id}/services` and `DELETE /projects/{id}/services/{serviceId}`
  unless both the project and the service are listed;
- host-wide routes: `GET /dashboard/stats`, `GET /backups`, `GET /git-sources`
  (and its repos and branches), and `POST /services`;
- `GET /audit-log` without a listed `resource_id`, or with `all_users=true`.

List routes (`/projects`, `/services`, `/containers`) return only listed
items, and `whoami` reports `token.restricted`, `token.project_ids`,
`token.service_ids`, and only listed services under `protected_services`.
Existing tokens are host-wide (`{}` lists, migration
`027_api_token_resource_scope`). There is no edit form: revoke and create a
new token to change the lists. Rotate keeps them.

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
| 403 | `resource_out_of_scope` | The token is restricted and the project, service, or container is not on its lists, or the route is host-wide (see [Restricting a token](#restricting-a-token-to-projects-and-services)) |
| 404 | `not_found` | Missing route, or a missing or another user's project, environment, or deployment |
| 409 | `conflict` | The deployment or service is busy or in the wrong state |
| 409 | `in_progress` | The same `approval_ref` already started this change on this target and it has not finished; `audit_id` names it |
| 422 | `immutable_field` | A config edit changes an immutable field |
| 500 | `internal_error` | Includes a failed audit insert; nothing was changed |
| 503 | `unavailable` | A dependency the route needs is not configured |
| 504 | `timeout` | An approved change did not finish while the request waited; it keeps running and `audit_id` names it |

Every answer under `/api/v1` and `/mcp` is JSON, including the router's
own 15 second request timeout (`503`, code `timeout`).

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
2. Ownership of the target (`404` or `403`), then a restricted token's
   lists (`403 resource_out_of_scope`).
3. Protected refusal (`403 protected_resource`), before any Docker call. This
   applies to dry runs too.
4. Dry run: answer the plan.
5. `approval_ref` check (`400 approval_ref_required`).
6. Replay check: if this user already ran this action on this target with
   this `approval_ref`, answer as that run (see Retries) and do nothing.
7. Insert `audit_logs` row `api_v1.<action>` with `details` =
   `{via, approval_ref, idempotency_key, token_id, token_name, target, changes}`.
   If the insert fails the API answers `500` and does nothing.
8. Run the change on a context of its own, so a client that leaves or a
   request timeout does not stop it halfway. When it ends, an
   `api_v1.<action>.completed` row (with `result` and `operation`) or an
   `api_v1.<action>.failed` row (with `error` and `status`) is added with
   `details.audit_id` set to the row from step 7.

If the change is still running about 12 seconds in (2 seconds before the
request deadline), the API answers `504` with code `timeout` and the
`audit_id`. The change keeps running (up to 10 minutes) and writes its
outcome row as above.

### Retries

One `approval_ref` runs a given action on a given target once. The target is
the resource id plus, where present, the variable `key`, the linked
`service_instance_id`, or the `template` and `name` of a new service, so one
approval may still cover several variables. A retry with the same
`approval_ref` answers:

- `200` with the first run's `result`, `operation`, and `audit_id`, and
  `"replayed": true`, when the first run finished;
- the first run's error status and code, with its `audit_id` and
  `"replayed": true`, when it failed;
- `409 in_progress` with its `audit_id` while it is still running.

A unique index (migration 023) keeps two concurrent retries from both
running. A new attempt needs a new `approval_ref`. The replay check runs
after the handler's own state checks, so a retry of a change that moved the
target on (a cancel, a rollback) may answer `409 conflict` instead of the
replay; it still runs nothing.

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

Work that goes on after the answer adds an `operation` to pass to the MCP
`wait_for` tool, or to poll with the matching read route:

```json
"operation": {"kind": "deployment", "id": "…", "project_id": "…", "poll_tool": "wait_for"}
```

`trigger_deployment` returns a `deployment` operation; `deploy_service`,
`update_service_image`, and `update_service_config` return a `service`
operation.

`target` and `changes` never hold a secret value.

## Protected services

A service is protected when its ID is in `GOFORGE_PROTECTED_SERVICES`
(comma-separated service instance IDs, read at start) or when the
**Protected** switch on its service page is on (stored on the service,
applies at once, audited as `service.set_protected`). `GET /services` and
`GET /services/{id}` show `"protected": true`, and `GET /whoami` returns the
environment list followed by the caller's services protected with the
switch. If GoForge cannot read a service while checking, it treats the
service as protected.

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
| `GET` | `/api/v1/whoami` | User, token scopes and resource lists (`restricted`, `project_ids`, `service_ids`), grants, protected list |
| `GET` | `/api/v1/dashboard/stats` | Your project, service, and container totals |
| `GET` | `/api/v1/projects` | Your projects |
| `GET` | `/api/v1/projects/{id}` | One project with environments and linked services |
| `GET` | `/api/v1/projects/{id}/environments` | Its environments |
| `GET` | `/api/v1/projects/{id}/environments/{envId}/variables` | Variables; secret values are `••••••••` |
| `GET` | `/api/v1/projects/{id}/deployments?env_id=&limit=` | Deployments, newest first (limit 20, max 500) |
| `GET` | `/api/v1/projects/{id}/deployments/{deploymentId}` | One deployment |
| `GET` | `/api/v1/projects/{id}/deployments/{deploymentId}/logs?tail=` | Last log lines (200, max 5000) |
| `GET` | `/api/v1/service-templates` | Templates |
| `GET` | `/api/v1/service-templates/{name}` | One template; secret defaults are omitted |
| `GET` | `/api/v1/services` | Your service instances with a `protected` flag |
| `GET` | `/api/v1/services/{id}` | Masked config and connection info, immutable fields, named volumes, container state, and `last_error` (why the last deploy, start, or recreate failed; known secrets redacted, at most 500 characters; cleared on the next success) |
| `GET` | `/api/v1/containers` | Your tracked containers |
| `GET` | `/api/v1/containers/{id}` | State, ports, mounts, limits, networks. No env, no command, labels limited to Traefik routing |
| `GET` | `/api/v1/containers/{id}/logs?tail=&since=` | Last log lines (200, max 5000, 2 MB). Known service secrets are redacted |
| `GET` | `/api/v1/containers/{id}/stats` | One CPU, memory, network sample |
| `GET` | `/api/v1/audit-log?limit=&offset=&resource_type=&resource_id=&action=&all_users=` | Your audit rows (limit 50, max 500). `all_users=true` works for admins only |
| `GET` | `/api/v1/backups` | Backup status, admin only (403 otherwise): `enabled`, `configured_by` (`settings` or `environment`), `bucket`, `prefix`, `schedule`, `retention_daily`, `next_run_at`, `last_run`, `last_success`, `recent` (10 newest `backup_runs` rows). Never returns credentials or the endpoint. There is no run-now write in v1 |
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
| `POST` | `/api/v1/projects/{id}/services` | `service_instance_id` | Links and injects connection variables; the plan lists keys added or skipped. The result has `linked`, `added_keys`, `failed_keys`, and `partial` (true when a key could not be written) |
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
