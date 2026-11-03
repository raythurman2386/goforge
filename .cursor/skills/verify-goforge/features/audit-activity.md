# Audit visibility in Settings → Activity

Every change made in the web UI, through `/api/v1`, or by an MCP agent is a row on Settings → Activity, newest first, with its source, action, token name, resource link, `approval_ref`, and error. Failed rows are tinted red. A queued deploy or service operation that fails later writes a `.failed` outcome row whose `details.audit_id` is the row that started it. The dashboard's Recent agent actions card shows the latest MCP and API rows.

## Sub-features

- `activity-page` returns `200` on `GET /settings/activity` with the heading `Activity` and the table `#activity-table`.
- `activity-api-row` shows an `api_v1.restart_service` row with the token name, the `API` badge, the `approval_ref`, and a link to `/services/$ID`.
- `activity-ui-delete` writes `service.delete` with `details.via` = `ui` when a service is deleted in the UI, and the row shows the `UI` badge.
- `activity-filters` narrows rows by `source`, `token`, and `action`, and `audit_id` shows a change with its outcome rows.
- `activity-failed-outcome` shows a failed queued deploy as `deployment.run.failed`, tinted red (`data-failed="true"`), with `details.audit_id` equal to the `api_v1.trigger_deployment` row's id and the same `token_name`.
- `activity-own-rows` shows a second account none of the first account's rows; `all=1` is ignored for a non-admin.
- `activity-dashboard-card` returns `200` on `GET /dashboard/activity` with `id="recent-agent-actions"` and only MCP or API rows.
- `audit-git-source` (v0.5.0 S3) `POST /settings/git` writes `git_source.create` and `DELETE /settings/git/{id}` writes `git_source.delete`, both `resource_type` `git_source`, `via` = `ui`, with `name`, `type`, `has_access_token`, `has_api_url`, and never the token or URL. Another account's delete is `403` and writes nothing.
- `audit-profile` (v0.5.0 S3) `PUT /settings/profile` writes `user.profile.update` with `changed` and `old_email_domain` / `new_email_domain`; the full address is never in `details`.
- `audit-webhook-deploy` (v0.5.0 S3) a signed push to an auto-deploy branch writes `deployment.trigger` with `via` = `webhook`, `provider`, `branch`, `commit`, the project owner as `user_id`, and the queue item's `audit_id` set to it, so `deployment.run.*` links back. The row shows the `Webhook` badge; `?source=webhook` lists it and `?source=ui` does not.
- `audit-retention` an admin sets retention on Settings → Profile. `0` keeps rows. `30` to `3650` deletes older rows daily. `10` and `abc` are field errors. A non-admin does not see the card and gets `403` on `PUT /settings/audit-retention`. See [audit-retention.md](./audit-retention.md).
- `audit-integrity` sealed rows reject `UPDATE`, `DELETE`, and `TRUNCATE`. `goforge audit verify` exits 0 on a clean chain and 1 after a bypassed edit or delete. See [audit-integrity.md](./audit-integrity.md).

## How to get to it (user POV)

- Sidebar, settings group: **Activity**.
- Use the Source, Token name, and Action fields and choose **Filter**. **Clear** resets them.
- Choose an action to see that change and its outcome rows.
- The dashboard loads **Recent agent actions** below the stats; **View all** opens the page filtered to agents.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes`. `activity-ui-delete` and `activity-failed-outcome` also need `drive_docker=yes`.
- `jar-a.txt` is the admin owner and `jar-b.txt` the second account from [container-ownership.md](./container-ownership.md).
- `token-write` exists from [api-v1.md](./api-v1.md) (token name `verify-write`), and `verify-pg` is deployed. `$ID` is `verify.sh psql "select id from service_instances where name = 'verify-pg'"`.
- Read `goforge_csrf` from the jar into `$csrf`. Do not print it, the token, or a cookie.

- **API row.** `POST /api/v1/services/$ID/restart` with `token-write` and `{"dry_run":false,"approval_ref":"verify-activity-1"}`. Save `audit_id`. Then:

```bash
curl -sS -b /tmp/goforge-verify/state/jar-a.txt \
  "http://127.0.0.1:8080/settings/activity?source=api&token=verify-write&action=restart_service" \
  -o /tmp/goforge-verify/state/activity-api.html -w '%{http_code}\n'
```

The status is `200`. The body has `api_v1.restart_service`, `>verify-write<`, `verify-activity-1`, `href="/services/$ID"`, `>API<`, and `data-activity-id="$AUDIT_ID"`.

- **Filters.** The same URL with `source=ui` has no `data-activity-id="$AUDIT_ID"`. `?audit_id=$AUDIT_ID` has that row and the `api_v1.restart_service.completed` outcome row.
- **UI delete.** Deploy a throwaway service `verify-activity-redis` from the catalog, read its id `$RID`, then `curl -X DELETE -H "X-CSRF-Token: $csrf" -b jar-a.txt "http://127.0.0.1:8080/services/$RID"`. `verify.sh psql "select action, details->>'via' from audit_logs where resource_id = '$RID' order by created_at desc limit 1"` returns `service.delete|ui`. `GET /settings/activity?source=ui&action=service.delete` shows the row with `>UI<`.
- **Failed outcome.** With `token-write`, `POST /api/v1/projects/$BROKEN/deployments` with `{"dry_run":false,"approval_ref":"verify-activity-2"}` on a verify project whose repository URL does not exist. Save `audit_id`. Poll `verify.sh psql "select action, details->>'token_name', details->>'approval_ref' from audit_logs where details->>'audit_id' = '$AUDIT_ID' and action like 'deployment.run.%'"` every 5 seconds for 3 minutes; it returns `deployment.run.failed|verify-write|verify-activity-2`. `GET /settings/activity?audit_id=$AUDIT_ID` has `data-failed="true"`. Through `/mcp` (`verify.sh drive-mcp-http`) the same row shows `>MCP<`.
- **Own rows.** With `jar-b.txt`, `GET /settings/activity?all=1` has no `data-activity-id="$AUDIT_ID"`.
- **Card.** With `jar-a.txt`, `GET /dashboard/activity` is `200`, has `id="recent-agent-actions"`, and has no `>UI<` badge.
- **Git source (S3, host mode OK).** Read `goforge_csrf` from the jar. `curl -b jar -X POST /settings/git -H "X-CSRF-Token: $csrf" --data-urlencode name=verify-gs --data-urlencode type=gitea --data-urlencode api_url=https://gitea.verify.test/api/v1 --data-urlencode access_token@<token file>` is `201` with `HX-Redirect: /settings/git`. `verify.sh psql "select id from git_sources where name = 'verify-gs'"` gives `$GS`. `curl -X DELETE -H "X-CSRF-Token: $csrf" /settings/git/$GS` is `200`. `verify.sh psql "select action, resource_id, details->>'name', details->>'type', details->>'has_api_url', details->>'via' from audit_logs where resource_type = 'git_source' order by created_at"` returns `git_source.create|$GS|verify-gs|gitea|true|ui` then `git_source.delete|...` (keep `token` out of the SQL text: `verify.sh psql` refuses it). `verify.sh psql "select count(*) from audit_logs where details::text like '%gitea.verify.test%'"` is `0`.
- **Profile (S3).** `curl -X PUT /settings/profile -H 'HX-Request: true' --data-urlencode csrf_token=$csrf --data-urlencode email=<local>@renamed.verify.test` is `200`. `verify.sh psql "select details->>'changed', details->>'old_email_domain', details->>'new_email_domain' from audit_logs where action = 'user.profile.update' order by created_at desc limit 1"` shows `["email"]|<old domain>|renamed.verify.test`. Put the address back the same way.
- **Webhook deploy (S3).** Needs a project whose `repository_url` equals the push's `repository.html_url` and an environment with `auto_deploy` on whose branch (or the project default) matches. Create the project in the UI, then `verify.sh psql "select e.id, e.auto_deploy from environments e join projects p on p.id = e.project_id where p.name = 'verify-hook'"`. A new project's `production` environment has `auto_deploy` off and no page toggles it, so on a throwaway database turn it on with a direct `update environments set auto_deploy = true, branch = 'main' where id = '<env id>'` through `docker exec ... psql` (not the live stack) and say so in the proof. Sign the body with the stack's `WEBHOOK_SECRET` (read it from the mode-600 env file into a shell variable; never print it): `sig=$(printf %s "$body" | openssl dgst -sha256 -hmac "$secret" -hex | sed 's/^.* //')`, then `curl -X POST /webhooks/github -H 'X-GitHub-Event: push' -H "X-Hub-Signature-256: sha256=$sig" --data-binary "$body"` is `200` `{"status": "accepted"}`. `verify.sh psql "select id, user_id, details->>'via', details->>'provider', details->>'branch', details->>'commit' from audit_logs where action = 'deployment.trigger' order by created_at desc limit 1"` shows `webhook|github|main|<sha>` and the owner's id. The deploy fails fast without Docker (host mode) or a reachable repo; poll `verify.sh psql "select action from audit_logs where details->>'audit_id' = '<id>'"` for `deployment.run.failed`. `GET /settings/activity?source=webhook` has `>Webhook<` and `deployment.trigger`.
- **Recent actions.** `verify.sh psql "select action, details->>'via' from audit_logs order by created_at desc limit 8"`.
- **Proof.** Write the feature id, status, and row check of each step to `/tmp/goforge-verify/proof/audit-activity-verdict.txt`. Never write a token, a cookie, or a config value.

## Gotchas

- The CSRF token is bound to the session. Right after register or login the jar still holds the anonymous token, so the first POST or DELETE gets `403 invalid CSRF token`. Do a `GET` (for example `/settings/git`) first, then re-read `goforge_csrf`. A refused create writes no audit row.
- Written from source with unit coverage (`TestActivityPage_RendersAPIRestartRowWithTokenName`, `TestServiceHandler_Delete_WritesAuditRow`, `TestWorker_FailedMCPDeployWritesLinkedFailedRow`). The S3 rows have `TestSettingsHandler_GitSource_WritesAuditRows`, `TestSettingsHandler_UpdateProfile_WritesAuditRow` (both need `DATABASE_URL`), `TestProcessPushEvent_*` in `internal/webhook/webhook_audit_test.go`, `TestActivityPage_RendersS3AuditRows`, and the `webhook` cases in `TestAuditLogRepository_ListFiltered_Activity`; the Git source, profile, and webhook steps were driven in host mode in t1870u.
- Source is derived, not stored: `MCP` when the user agent starts with `goforge-mcp`, `API` for `api_v1.` actions or `details.via` = `api_v1`, `Webhook` for `details.via` = `webhook`, else `UI`. A curl call to `/api/v1` is `API`, not `MCP`.
- `deployment.run.completed` is written when the queue worker finishes the pipeline; the deployment row itself may read `running`.
- Webhook-triggered deploys write `deployment.trigger` (`via` = `webhook`) since v0.5.0 S3, so their `deployment.run.*` row links back and inherits `via`, user, IP, and user agent. A push to a branch no auto-deploy environment builds writes nothing.
- `verify.sh psql` refuses SQL containing `password` and other secret-looking words. Use `like 'user.%'` for account rows.
- Retention defaults to forever (`0`). `GOFORGE_AUDIT_RETENTION_DAYS` applies only until an admin saves the Profile card. Do not expect rows to disappear unless retention is set to 30 or more. The page shows the rule in `data-testid="audit-retention-note"`.
- `UI` rows never hold variable values or service config values: `env_var.set` has the key and `is_secret`, `service.update_config` the field names.
- Only on the verify stack, never live.
