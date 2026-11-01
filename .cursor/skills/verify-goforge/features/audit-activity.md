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
- **Proof.** Write the feature id, status, and row check of each step to `/tmp/goforge-verify/proof/audit-activity-verdict.txt`. Never write a token, a cookie, or a config value.

## Gotchas

- Written from source with unit coverage (`TestActivityPage_RendersAPIRestartRowWithTokenName`, `TestServiceHandler_Delete_WritesAuditRow`, `TestWorker_FailedMCPDeployWritesLinkedFailedRow`); not driven live yet.
- Source is derived, not stored: `MCP` when the user agent starts with `goforge-mcp`, `API` for `api_v1.` actions or `details.via` = `api_v1`, else `UI`. A curl call to `/api/v1` is `API`, not `MCP`.
- `deployment.run.completed` is written when the queue worker finishes the pipeline; the deployment row itself may read `running`.
- Webhook-triggered deploys have no starting audit row, so they write no outcome row.
- `UI` rows never hold variable values or service config values: `env_var.set` has the key and `is_secret`, `service.update_config` the field names.
- Only on the verify stack, never live.
