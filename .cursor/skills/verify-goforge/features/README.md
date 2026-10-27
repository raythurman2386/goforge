# GoForge verification map

This directory is the maintained source for verifying the user-facing behavior of GoForge. Read this index, then use the matching feature file.

## Baseline preconditions

- One Compose project, `goforge-verify`, started by `.cursor/skills/verify-goforge/scripts/verify.sh launch`.
- UI base URL `http://127.0.0.1:8080`.
- State directory `/tmp/goforge-verify` holds `stack.env` and cookie jars. Evidence does not.
- Evidence directory `artifacts/verify-goforge-proof/` at the repo root.
- `verify.sh doctor` reports `drive_auth=yes` before any drive.
- Deploy, container, and service drives also need `drive_docker=yes`.
- Never drive an instance that this verification run did not start.

## Driving conventions

- Start from the baseline state unless the feature file adds a precondition.
- The UI is HTMX. Drive it with curl against the form routes and headings named in the feature file. There is no Playwright suite in this repo.
- Treat every command as literal. Keep paths, field names, and status values unchanged.
- Session and CSRF values stay in `/tmp/goforge-verify`. Do not print them and do not copy jars into the evidence directory.
- A second view of a mutation is SQL through `verify.sh psql`, or a later GET with the same cookie.
- Cleanup may remove containers, volumes, and `/tmp/goforge-verify`. It must leave the evidence directory in place.

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

- [Register the bootstrap account](./register-admin.md) covers the first account, the dashboard session, and registration closing after that account exists.
- [Deploy with log replay](./deploy-log-replay.md) covers creating a project, starting a deploy, and seeing the same log lines after a reconnect.
- [Container stop and restart ownership](./container-ownership.md) covers stop and restart for the owning account and denial for a second account.
- [Cancel a deployment](./cancel-deployment.md) covers canceling an in-flight deploy and the status staying `canceled`.
- [One-click service credentials](./service-credentials.md) covers a Postgres service whose stored config and connection info are sealed, while the UI still shows the connection string.
