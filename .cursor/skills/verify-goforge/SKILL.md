---
name: verify-goforge
description: "Drive the GoForge HTMX UI on a single Docker Compose stack and prove registration, the admin grant, login and projects, deploy log replay, container ownership, deployment cancel, one-click service credentials, backup settings, and MCP over HTTP with Profile tokens. Use when verifying GoForge, reproducing a UI bug, or checking a change against the feature map."
---

# Verify GoForge

The primary surface is the HTMX UI served by `goforge serve` inside Docker Compose. `make dev` is for editing Go. It does not attach the Docker socket the deploy and service journeys need. There is no second node. One Compose project named `goforge-verify` is the only instance this skill may drive.

Read [features/README.md](features/README.md) before a drive. Then follow that feature file. A journey that names a missing precondition fails and the reason goes in the evidence file. Do not change the expected status, row shape, or log replay to match the current tree.

## Paths

- State: `/tmp/goforge-verify/state`. Holds `stack.env`, cookie jars, and fixture passwords. Mode `700`. Never copy it into evidence.
- Evidence: `/tmp/goforge-verify/proof`. Override with `GOFORGE_VERIFY_PROOF`. The helper canonicalizes with `realpath -m` and refuses a path inside the repo. **Never commit proofs.** `artifacts/` is in `.gitignore`.

## Launch

From the repo root:

```bash
.cursor/skills/verify-goforge/scripts/verify.sh launch
```

If the user is not in the `docker` group (for example on minis, where the socket is `root:docker` and passwordless sudo works), prefix every helper call with `GOFORGE_VERIFY_DOCKER="sudo -n docker"`.

The helper writes a mode `600` env file at `/tmp/goforge-verify/state/stack.env` and starts Compose with `-f docker-compose.yml --project-directory <repo> --env-file ... -p goforge-verify`. It does not read or modify a repo `.env`. If `$ROOT/.env` exists, launch refuses.

The stack env matches the production defaults from #28:

- `GOFORGE_DEV_MODE=false`. Compose uses `${VAR:?}` for `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, `DATABASE_URL`, and `POSTGRES_PASSWORD`, so empty values do not start. The helper generates them with `openssl rand` per launch.
- `GOFORGE_ENABLE_REGISTRATION=false` (default). Override with `GOFORGE_VERIFY_ENABLE_REGISTRATION=true` only for a drive that needs a second account, and say so in the verdict.
- `GOFORGE_ADMIN_EMAIL=verify-admin@example.test`. Override with `GOFORGE_VERIFY_ADMIN_EMAIL`.

Do not print `stack.env`.

`cmd/goforge/main.go` runs migrations before `server listening`. Ready is `GET http://127.0.0.1:8080/health/ready` with `"database":"ok"`. The helper waits up to 10 minutes after `docker compose up -d --build` returns. A cold build on minis takes a few minutes.

`docker-compose.yml` publishes the app on `127.0.0.1:8080` only, Traefik on `80` and `443`, and the Traefik dashboard port on `127.0.0.1:8081` (the dashboard is off; `TRAEFIK_DASHBOARD_INSECURE=false`). Postgres is not published. It also creates a network named `traefik`. Two stacks cannot run side by side. Launch exits `1` when `80`, `443`, `8080`, or `8081` is listening, when the `traefik` network already exists, or when project `goforge-verify` already has containers. Do not drive an instance this run did not start.

A fresh volume has zero users. The first registration is always allowed. After that, registration is closed and `/auth/register` returns `403`. Every account is created as `user`. The account whose email matches `GOFORGE_ADMIN_EMAIL`, ignoring case, is created as `admin`, and startup promotes an existing match.

## Host mode

Use host mode when `launch` refuses because `127.0.0.1:8080` is taken. On minis the live `goforge-goforge-1` holds it. Do not stop the live stack. Build the branch and run `goforge serve` on the host against a throwaway Postgres container:

- Postgres: `postgres:17-alpine` published on a free `127.0.0.1` port, with a fresh database. Never a live database.
- Serve: `GOFORGE_PORT=18080`, `GOFORGE_HOST=127.0.0.1`, `GOFORGE_BASE_URL=http://127.0.0.1:18080`, random `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, and `WEBHOOK_SECRET` in a mode `600` env file, `GOFORGE_ENABLE_REGISTRATION=true`, `GOFORGE_ADMIN_EMAIL=verify-admin@example.test`, `LOG_FORMAT=json`, and stdout to `/tmp/goforge-verify/host-app.log`.
- Point the drives at it with `GOFORGE_VERIFY_BASE`, `GOFORGE_VERIFY_PSQL` (a psql command for that database; `-c <sql>` is appended), and `GOFORGE_VERIFY_APP_LOGS` (a command that prints the app log).
- Create `/tmp/goforge-verify/state` with mode `700` first. `launch` normally makes it.
- Skip `doctor`. It reads the Compose project. Docker is not reachable from a host serve run as a non-root user, so `/health/ready` reports `not_ready` with `"database":"ok"`. Auth, token, and MCP drives only need the database.
- Stop the host serve by its PID, not by process name. Then remove the Postgres container with `-v`.

[features/backups.md](features/backups.md) and [features/mcp-http.md](features/mcp-http.md) give the exact commands. With registration open, the `register-closed*` and `register-admin-role` lines in `register-verdict.txt` read `fail`. That is expected in host mode.

## Doctor

```bash
.cursor/skills/verify-goforge/scripts/verify.sh doctor
```

Doctor is read-only. It is worth driving when all of these hold:

- Project `goforge-verify` has a running `goforge` container.
- `GET /health` returns `200` and `"status":"healthy"`.
- `GET /health/ready` returns `"database":"ok"`.
- `docker compose port goforge 8080` is `127.0.0.1:8080` (`app_loopback=yes`).

Doctor also records the `docker` check and `docker compose port db 5432`. Postgres is not published, so `db_port=unpublished` is expected. Deploy and service drives need `"docker":"ok"` as well. Auth drives need only the database check. Exit `1` when the instance is not worth driving.

## Drive

Run doctor, then one feature file. Cookies and tokens stay in `/tmp/goforge-verify/state`. Never print `stack.env`, cookie jars, `goforge_csrf`, `goforge_session`, or passwords.

Stable handles:

- CSRF cookie `goforge_csrf`, form field `csrf_token`, header `X-CSRF-Token`. State-changing routes reject a missing token with `403` and `invalid CSRF token`.
- Session cookie `goforge_session`.
- Register form `POST /auth/register` with `email`, `password`, `confirm_password`. The heading is `Create your account`. The submit button is `Register`. Closed registration is `403` `Registration is disabled` on both GET and POST.
- Login form `POST /auth/login` with `email`, `password`. The heading is `Sign in to GoForge`. Success is `303` to `/dashboard`.
- After register or login the browser lands on `/dashboard`. The heading is `Dashboard`.
- Projects list `GET /projects`, heading `Projects`, subtitle `View and manage your application projects.`
- New project form `POST /projects` with `name`, `repository_url`, `default_branch`, `dockerfile_path`, `build_context`. Success is `303` to `/projects/{id}`. The manual URL field is renamed to `repository_url` by page script. HTTP drives post `repository_url` directly.
- Deploy control is the `Deploy Now` button, `POST /projects/{id}/deployments?env_id={env}`.
- Cancel control is the `Cancel` button, `POST /projects/{id}/deployments/{deploymentID}/cancel`.
- Log pane id `deployment-logs`. Live stream is `GET /projects/{id}/deployments/{deploymentID}/logs` with `Content-Type: text/event-stream`. A reconnect sends one `snapshot` event of stored logs.
- Container stop id `stop-btn`, `POST /containers/{dockerID}/stop`. Restart id `restart-btn`, `POST /containers/{dockerID}/restart`.
- Service catalog link `Deploy` goes to `/services/{template}/configure`. Postgres posts to `POST /services/postgres`.
- Admin-only: `POST /dashboard/prune` and `POST /settings/prune`. A `user` gets `403` `forbidden`.

Authorization status codes after #28:

- Another account's project id, or a deployment id under a project it does not belong to (get, cancel, rollback, status, log stream, trigger with another project's `env_id`), is `404`. Do not expect `403` there.
- Another account's container (detail, logs, stats, stop, restart) is still `403` `access denied`.

Scripted journeys: `verify.sh drive-register` then `verify.sh drive-login`, then `drive-backup-settings` or `drive-mcp-http`. The other feature files give the curl and SQL for their journey. Rate limit is 30 requests per second with burst 60.

## Evidence

Write proof under `/tmp/goforge-verify/proof`. Capture the action and the resulting state.

- `launch.log` is the Compose up output.
- `doctor.txt` is the doctor report.
- `revision.txt` is `git rev-parse HEAD` at launch.
- One verdict file per drive, for example `register-verdict.txt` and `login-verdict.txt`.
- UI proof records HTTP status, the `Location` path, and whether a heading is present. Do not save HTML. Pages embed the CSRF token.
- Mutation proof is a second view. For register, that view is `select count(*)`, `email`, and `role` from `users` plus `GET /dashboard` with the session cookie.
- For services, evidence is `sealed_marker` and `plaintext_password_present` booleans from `verify.sh check-sealed`. Do not copy `config` or `connection_info` into the evidence directory.
- Mocks are out. The drive uses the Compose stack, the real form routes, and the real Postgres in the `db` service.
- Link the proof path in the PR body. Do not add proof files to the branch.

`verify.sh` scrubs evidence text for `DATABASE_URL`, secret assignments, and cookie values before it returns.

## Cleanup

```bash
.cursor/skills/verify-goforge/scripts/verify.sh cleanup
```

Cleanup runs `docker compose -p goforge-verify down -v --remove-orphans` for the env file this run wrote, then deletes `/tmp/goforge-verify/state`. It keeps `/tmp/goforge-verify/proof`. It compares the evidence file list before and after teardown and exits `1` if a file disappeared. It removes the `traefik` network only when that network has zero attached containers. It never kills by process name.

Run cleanup after every failed launch too, so ports and the `traefik` network do not stick.

## Helpers

The helper is `.cursor/skills/verify-goforge/scripts/verify.sh`.

```bash
.cursor/skills/verify-goforge/scripts/verify.sh launch
.cursor/skills/verify-goforge/scripts/verify.sh doctor
.cursor/skills/verify-goforge/scripts/verify.sh drive-register
.cursor/skills/verify-goforge/scripts/verify.sh drive-login
.cursor/skills/verify-goforge/scripts/verify.sh drive-backup-settings
.cursor/skills/verify-goforge/scripts/verify.sh drive-mcp-http
.cursor/skills/verify-goforge/scripts/verify.sh psql "select count(*) from users"
.cursor/skills/verify-goforge/scripts/verify.sh check-sealed verify-pg
.cursor/skills/verify-goforge/scripts/verify.sh cleanup
```

Environment overrides: `GOFORGE_VERIFY_DOCKER`, `GOFORGE_VERIFY_PROOF`, `GOFORGE_VERIFY_BASE`, `GOFORGE_VERIFY_PSQL`, `GOFORGE_VERIFY_APP_LOGS`, `GOFORGE_VERIFY_ADMIN_EMAIL`, `GOFORGE_VERIFY_REGISTER_EMAIL`, `GOFORGE_VERIFY_ENABLE_REGISTRATION`.

`psql` rejects SQL that mentions `password`, `secret`, `token`, `connection_info`, or `config`. `check-sealed` is the only command that reads those columns. It prints two booleans and deletes the row dump.

`drive-register` exits `0` when the harness finished and wrote `register-verdict.txt`. A product miss such as registration staying open or the role not being `admin` is a `fail` line inside that file, not a harness failure. Exit `2` when the account never reaches `/dashboard`. Exit `1` when launch or doctor is refused.

`drive-login` needs the password `drive-register` stored in the state directory. It logs in with the lowercase admin email, opens `/projects`, creates project `verify-smoke`, and checks that the list shows it. It writes `login-verdict.txt` and exits `2` only when login fails.

`drive-mcp-http` needs `drive-register` and `drive-login` first, and `GOFORGE_PROTECTED_SERVICES` set to one UUID on the stack. It mints tokens through the Profile form into mode `600` files in the state directory, sends them with `curl -H @file`, and deletes them before it exits. It writes `mcp-http-verdict.txt` and exits `2` when the harness could not run or a token value appears in the verdict. A failed sub-feature is a `fail` line, not an exit code.

## Maintenance

When the app changes, run `/maintain-verification-skill` against `.cursor/skills/verify-goforge/`. That pass re-reads each feature file and drives the live stack. It does not edit product code.
