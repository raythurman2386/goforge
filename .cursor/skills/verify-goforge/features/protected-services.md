# Protected services and typed-name delete

A service owner turns on **Protected** on a service page. From then on a `write` token cannot stop, reconfigure, or re-image that service through `/api/v1` or MCP (`403 protected_resource`), with no change to `GOFORGE_PROTECTED_SERVICES` and no restart. Each toggle writes a `service.set_protected` audit row. Deleting a service or a project needs its exact name typed in a dialog; the server refuses a delete without it.

## Sub-features

- `protected-toggle-on`: `POST /services/{id}/protected` with `protected=true&protected=false` answers `200` with the `#service-protected` card checked, and `select protected from service_instances where id = '$ID'` is `t`.
- `protected-toggle-audit`: the toggle writes one `service.set_protected` row with `details.via` `ui`, `details.protected` `true`, and `details.previous` `false`. Posting the same value again writes nothing.
- `protected-api-refusal`: with `token-write`, `POST /api/v1/containers/$DOCKER_ID/stop` (dry run and `{"dry_run":false}`), `PATCH /api/v1/services/$ID/config`, and `POST /api/v1/services/$ID/image` are each `403` `protected_resource`; the container is still running. `POST /api/v1/services/$ID/restart` is still allowed.
- `protected-reads`: `GET /api/v1/services/$ID` shows `"protected": true` and `GET /api/v1/whoami` lists `$ID` in `protected_services`.
- `protected-toggle-off`: `protected=false` answers `200`, stores `f`, writes a second row with `previous` `true`, and the `write` token's stop dry run is `200` again.
- `protected-env-locked`: for an ID in `GOFORGE_PROTECTED_SERVICES` the card is checked and disabled, and `protected=false` answers `409` naming the variable, with no audit row.
- `typed-delete-service`: `DELETE /services/$ID` without `confirm_name`, or with the wrong one, is `400` and the row stays. With `?confirm_name=<name>` it is `200` with `HX-Redirect: /services` and a `service.delete` row.
- `typed-delete-project`: the same for `DELETE /projects/$PID` with the project name; success redirects to `/projects`.

## How to get to it (user POV)

- Services → a service → **Protected** card under the header. The switch posts on change; the card text says where the protection comes from.
- **Delete** on a service page, or Project → Settings → **Delete Project**, opens a dialog. Type the name shown in bold, then **Delete service** or **Delete project**. A wrong name shows the server's message in the dialog.

## Driving it with verify.sh

- Preconditions: `drive-register`, `drive-login`, a running `verify-pg` service from this run, and Profile tokens `token-write` (read, write) as in [api-v1](./api-v1.md). Never toggle or delete a service this run did not start.
- `ID=$(verify.sh psql "select id from service_instances where name = 'verify-pg'")`. The CSRF header comes from the jar as in the other UI drives.
- Toggle: `curl -b $JAR -H "X-CSRF-Token: $CSRF" -d 'protected=true&protected=false' $BASE/services/$ID/protected`. Then run the API calls with `-H @token-write-header`.
- Audit: `verify.sh psql "select action, details->>'protected', details->>'previous', details->>'via' from audit_logs where resource_id = '$ID' and action = 'service.set_protected' order by created_at"`.
- Delete: `curl -X DELETE -b $JAR -H "X-CSRF-Token: $CSRF" "$BASE/services/$ID?confirm_name=wrong"` (expect `400`), then with the real name on a throwaway service only.
- No `verify.sh` subcommand drives this yet; the unit and handler tests below are the regression net, and t1663u drove it in host mode on minis.

## Gotchas

- The switch posts `protected=true` and a hidden `protected=false`; the handler treats any `true` as on.
- htmx sends a `DELETE`'s form fields in the query string, so `confirm_name` is read from the query first, then from a form body.
- The env list is read at start; the switch applies at once. For an env-listed service the switch is locked on, so only the env list can unprotect it.
- Tests: `TestUIProtectedFlagRefusesStop`, `TestUIProtectedFlagInReads`, `TestProtectedCheckFailsClosed` (apiv1), `TestServiceHandler_SetProtected_*`, `TestServiceHandler_Delete_RequiresTypedName`, `TestProjectHandler_Delete_RequiresTypedName` (handlers, the last needs `DATABASE_URL`), `TestServiceInstanceRepository_Protected`, `TestProtectedCardStates`, `TestDetailDeleteUsesTypedNameDialog`.
