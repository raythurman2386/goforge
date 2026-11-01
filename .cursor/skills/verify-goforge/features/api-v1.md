# JSON API v1 with scoped tokens

An admin creates an API token with `read`, `write`, or `protected` scopes on the profile page and calls `/api/v1` with it as a bearer token. Session cookies do not work there. Reads return masked secrets. Every mutation is a dry run unless the body says `"dry_run": false`, a real run needs `approval_ref`, and the audit log records it before anything changes. Stop, config edit, and image update of a protected service are refused without the `protected` scope; restart is allowed.

## Sub-features

- `api-token-scoped-create` creates a token with chosen scopes, and the row shows scope chips.
- `api-bearer-only` returns `401` `unauthorized` for a session cookie with no bearer token, and `401` for a scoped token on a UI route.
- `api-read-scope` returns `200` on reads and `403` `insufficient_scope` on mutations for a `read` token.
- `api-masked-reads` shows secret env vars and service passwords as `••••••••`.
- `api-dry-run-default` answers a mutation without `dry_run` with a plan and no change.
- `api-approval-required` answers `"dry_run": false` without `approval_ref` with `400` `approval_ref_required`.
- `api-audit-approval` writes an `api_v1.<action>` row whose `details.approval_ref` matches.
- `api-protected-refusal` answers stop, config edit, and image update of a protected service with `403` `protected_resource` for a `write` token, dry run included.
- `api-protected-restart` restarts a protected service with a `write` token.
- `api-ownership-parity` returns `404` for another account's project and `403` for its service or container.
- `api-json-timeout` answers an approved change still running after about 12 s with `504` JSON `error.code` `timeout` and `error.audit_id`. The change keeps running and writes `api_v1.<action>.completed` or `.failed` with `details.audit_id` set to that id. The router's 15 s timeout on `/api/v1` and `/mcp` also answers JSON `timeout` (`503`).
- `api-outcome-audit` writes, for every approved change, the begin row `api_v1.<action>` and one outcome row `api_v1.<action>.completed` (`details.result`, `details.operation`) or `api_v1.<action>.failed` (`details.error`, `details.status`).
- `api-service-last-error` shows `last_error` on `GET /api/v1/services/{id}` (and the service detail page, `id="service-last-error"`) after a failed deploy, start hook, or recreate. Known secrets are redacted, it is at most 500 characters, and it is cleared by the next successful start, restart, or recreate (migration 022).
- `api-operation` adds `operation` `{kind, id, project_id?, poll_tool:"wait_for"}` to `trigger_deployment`, `deploy_service`, `update_service_image`, and `update_service_config` answers.
- `api-link-failed-keys` returns `added_keys`, `failed_keys`, and `partial` from `POST /api/v1/projects/{id}/services`.
- `api-idempotent-approval`: the same `approval_ref`, action, and target run once. A second `trigger_deployment` with the same ref answers `200` with the first `result`, `operation`, and `audit_id` and `"replayed": true`, and no second deployment row is made. A retry while the first is still running is `409` `in_progress`; a retry of a failed run returns the first error. Migration 023 adds the unique index.

## How to get to it (user POV)

- Open `Settings` at `/settings/profile`, section `API Tokens`.
- Choose `Generate Token`. The modal `Generate API Token` has a name, an expiry, and the `read`, `write`, `protected` boxes. No box means a legacy `full` token.
- Choose `Generate Token`. The modal `API Token Created` shows the token once.
- Call `http://127.0.0.1:8080/api/v1/...` with `Authorization: Bearer <token>`.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes`. The protected and restart steps also need `drive_docker=yes`.
- `verify.sh drive-login` has left a session in `/tmp/goforge-verify/state/jar-a.txt`.
- For `api-masked-reads`, `api-protected-*`, and `api-ownership-parity`: a running `verify-pg` service from [service-credentials.md](./service-credentials.md), and a second account as in [container-ownership.md](./container-ownership.md).
- For the protected steps, mark `verify-pg` protected. Its id is `verify.sh psql "select id from service_instances where name = 'verify-pg'"`. Append `GOFORGE_PROTECTED_SERVICES=<id>` to `/tmp/goforge-verify/state/stack.env`, then recreate only the verify app container: `docker compose -f docker-compose.yml --project-directory . --env-file /tmp/goforge-verify/state/stack.env -p goforge-verify up -d goforge`. Never run it with `-p goforge`. Say so in the verdict.

- **Create tokens.** With `jar-a.txt` and header `X-CSRF-Token`, `POST /settings/tokens/` with form fields `name=verify-read`, `expires_in=1`, `scopes=read`. The status is `201` and `HX-Trigger` is `tokenCreated`. Take the token from the `<code>` element into `/tmp/goforge-verify/state/token-read`. Repeat for `scopes=write` (`token-write`) and `scopes=write&scopes=protected` (`token-protected`). `GET /settings/tokens/` shows the scope chips.
- **Bearer only.** `GET /api/v1/whoami` with only `jar-a.txt` is `401`. With `token-read` it is `200` and `scopes` is `["read"]`. `GET /projects` with `token-read` as a bearer is `401`.
- **Read scope.** `POST /api/v1/services/$ID/restart` with `token-read` is `403` and `error.code` is `insufficient_scope`.
- **Masked reads.** `GET /api/v1/services/$ID` with `token-read`: every secret config value is `••••••••`. Save only the field names and a `masked=true|false` boolean.
- **Dry run.** `POST /api/v1/services/$ID/restart` with `token-write` and no body is `200`, `dry_run` is `true`, and `StartedAt` of the container is unchanged (`docker inspect`).
- **Approval.** The same call with `{"dry_run":false}` is `400` `approval_ref_required`. With `{"dry_run":false,"approval_ref":"verify-1"}` it is `200` and `audit_id` is set. `verify.sh psql "select action, details->>'approval_ref' from audit_logs where id = '<audit_id>'"` returns `api_v1.restart_service|verify-1`.
- **Protected.** With `token-write`, `PATCH /api/v1/services/$ID/config` with `{"config":{"name":"verify-pg-renamed"}}` and `POST /api/v1/services/$ID/image` are each `403` `protected_resource`. With `token-protected` the dry run of each is `200` with `dry_run` `true`; the config dry run's `changes` names only `name`, and `verify.sh psql "select name from service_instances where id = '$ID'"` still returns `verify-pg`. The restart above is the `api-protected-restart` pass.
- **Ownership.** With a `read` token for the second account, `GET /api/v1/projects/<A's project>` is `404` and `GET /api/v1/services/$ID` is `403`.
- **Idempotent approval.** With `token-write`, `POST /api/v1/projects/$PROJECT/deployments` with `{"dry_run":false,"approval_ref":"verify-idem-1"}` twice. Both are `200` with the same `audit_id` and `result.id`; the second has `"replayed": true`. `verify.sh psql "select count(*) from deployments where id = '<result.id>'"` is `1`, and `verify.sh psql "select count(*) from audit_logs where action = 'api_v1.trigger_deployment' and details->>'approval_ref' = 'verify-idem-1'"` is `1`. Only on the verify stack, never live.
- **Outcome row.** `verify.sh psql "select action from audit_logs where details->>'audit_id' = '<audit_id>'"` returns `api_v1.restart_service.completed` for the approval step above.
- **Proof.** Write the feature id, status, and `error.code` of each step to `/tmp/goforge-verify/proof/api-v1-verdict.txt`. Never write a token, a cookie, or a config value.

## Gotchas

- Token files live in `/tmp/goforge-verify/state`, not the proof directory.
- `/api/v1` is outside the CSRF group. A missing CSRF header is not a failure there; a session cookie that works is.
- `full` tokens (no box checked, and every token from before migration 018) work on `/api/v1` with `read` and `write`, never `protected`.
- `GOFORGE_PROTECTED_SERVICES` is read at start. Changing it needs the verify app container recreated, as in the preconditions.
- Projects, environments, and deployments of another account are `404`; services and containers are `403`. Same as the UI.
- The rate limit is per client IP and shared with the UI.
- Reusing an `approval_ref` from an earlier step for the same action and target replays that run instead of acting. Use a fresh ref per step.
- `api-json-timeout`, `api-service-last-error`, and `api-link-failed-keys` are covered by unit tests (`internal/apiv1` `run_test.go`, `last_error_test.go`, `link_test.go`; `internal/services` `instance_last_error_test.go`); forcing them on a stack needs a slow or failing Docker.
- The protected config dry run needs a field that can change. `{"config":{}}` is `400` `bad_request` (config must name a field). Every Postgres template field (`database`, `username`, `password`) is immutable, so changing one is `422`. `name` is editable on every template, so it is the field to use.
