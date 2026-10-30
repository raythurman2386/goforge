# GoForge verification map

This directory is the maintained source for verifying the user-facing behavior of GoForge. Read this index, then use the matching feature file.

## Baseline preconditions

- One Compose project, `goforge-verify`, started by `.cursor/skills/verify-goforge/scripts/verify.sh launch`.
- UI base URL `http://127.0.0.1:8080`.
- State directory `/tmp/goforge-verify/state` holds `stack.env`, cookie jars, and fixture passwords.
- Evidence directory `/tmp/goforge-verify/proof`, outside the repo. Never commit it.
- Registration is closed after the first account and `GOFORGE_ADMIN_EMAIL=verify-admin@example.test` unless the launch overrode them.
- `verify.sh doctor` reports `drive_auth=yes` before any drive.
- Deploy, container, and service drives also need `drive_docker=yes`.
- Never drive an instance that this verification run did not start.

## Driving conventions

- Start from the baseline state unless the feature file adds a precondition.
- The UI is HTMX. Drive it with curl against the form routes and headings named in the feature file. There is no Playwright suite in this repo.
- Treat every command as literal. Keep paths, field names, and status values unchanged.
- Session and CSRF values stay in `/tmp/goforge-verify/state`. Do not print them and do not copy jars into the evidence directory.
- A second view of a mutation is SQL through `verify.sh psql`, or a later GET with the same cookie.
- Cleanup may remove containers, volumes, and `/tmp/goforge-verify/state`. It must leave `/tmp/goforge-verify/proof` in place.

## Proof and skip reporting

- Capture the user action and the resulting state, not only the final screen.
- Record the feature id, the HTTP status, and the heading or row check in the evidence file for that feature.
- A product miss stays a failed sub-feature. Do not retarget the expected status so the run looks green.
- Report an unreachable path with the command that was run and the unmet precondition.
- Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 and one paragraph of user-visible behavior. It then uses these four H2 sections in order.

1. `Sub-features`
2. `How to get to it (user POV)`
3. `Driving it with verify.sh`
4. `Gotchas`

## Features

Last driven live on minis after #28: register-admin and login-projects. The other feature files were updated from source after #28 and have not been re-driven since.


- [Register the bootstrap account](./register-admin.md) covers the first account, the dashboard session, the `GOFORGE_ADMIN_EMAIL` admin grant, and registration closing after that account exists.
- [Log in and create a project](./login-projects.md) covers login, the projects list, and creating a project.
- [Deploy with log replay](./deploy-log-replay.md) covers creating a project, starting a deploy, and seeing the same log lines after a reconnect.
- [Container stop and restart ownership](./container-ownership.md) covers stop and restart for the owning account and `403` for a second account.
- [Cancel a deployment](./cancel-deployment.md) covers canceling an in-flight deploy and the status staying `canceled`.
- [One-click service credentials](./service-credentials.md) covers a Postgres service whose stored config and connection info are sealed, while the UI still shows the connection string.
- [Service and project metrics after a config save](./service-metrics.md) covers the edit-save pending poll stopping once `running`, the stats stream first frame within about 1 s, the `unavailable` event for a stopped or missing container, the stream kept across polls by Docker ID, the `Container` card on service detail and project Overview, and the project empty state. Written from source with unit coverage; not driven live yet.
- [JSON API v1 with scoped tokens](./api-v1.md) covers scoped token creation, bearer-only access, masked reads, dry run by default, `approval_ref` in the audit log, protected-service refusal, and ownership parity. Written from source; not driven live yet.
- [GoForge MCP over HTTP](./mcp-http.md) covers `/mcp` on serve: bearer-only auth with Profile tokens, `full` and cross-origin refusal, read and write tool inventory, the write-scope tool error, dry run, `approval_ref` and client IP in the audit log, protected refusal, and the Profile MCP card with create, rotate, revoke, and their audit rows. `verify.sh drive-mcp-http`, driven in host mode on minis.
- [GoForge MCP tools (stdio fallback)](./mcp-tools.md) covers `cmd/goforge-mcp`, the stdio server that calls `/api/v1` over HTTPS: tool inventory, a read call, a dry run, and protected refusal. Written from source; not driven live yet.
- [Restore the GoForge database](./backup-restore.md) covers `goforge backup list`, `goforge backup restore` (dry run, confirm, every refusal, side database and rename, kept previous database, sessions cleared, audit row, PostgreSQL 15 target), and the read-only Restore points list on Settings → Profile. Integration test on PG15 and PG17 plus a host-mode drive on minis.
- [Nightly S3 backups](./backups.md) covers a run to S3-compatible storage, skips, retention, secret redaction, and the admin-only status on Settings, the API, and MCP. Smoke is the integration test against throwaway MinIO; no verify.sh drive yet.
