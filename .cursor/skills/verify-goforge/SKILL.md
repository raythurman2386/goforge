---
name: verify-goforge
description: "Drive the GoForge HTMX UI on a single Docker Compose stack and prove registration, deploy log replay, container ownership, deployment cancel, and one-click service credentials. Use when verifying GoForge, reproducing a UI bug, or checking a change against the feature map."
---

# Verify GoForge

The primary surface is the HTMX UI served by `goforge serve` inside Docker Compose. `make dev` is for editing Go. It does not attach the Docker socket the deploy and service journeys need. There is no second node. One Compose project named `goforge-verify` is the only instance this skill may drive.

Read [features/README.md](features/README.md) before a drive. Then follow that feature file. A journey that names a missing precondition fails and the reason goes in the evidence file. Do not change the expected status, row shape, or log replay to match the current tree.

## Launch

From the repo root:

```bash
.cursor/skills/verify-goforge/scripts/verify.sh launch
```

The helper writes a mode `600` env file at `/tmp/goforge-verify/stack.env` and starts Compose with `--env-file` and `-p goforge-verify`. It does not read or modify a repo `.env`. If `$ROOT/.env` exists, launch refuses.

Launch uses the documented dev stack. `GOFORGE_DEV_MODE=true`, `DATABASE_URL` pointing at the `db` service, and empty `ENCRYPTION_KEY`, `SESSION_SECRET`, and `CSRF_SECRET`. In dev mode the process generates those secrets after migrations. Do not print `stack.env`.

`cmd/goforge/main.go` runs `database.MigrateUp` before `server listening`. Ready is not the process start. Ready is `GET http://127.0.0.1:8080/health/ready` with `"database":"ok"`. The helper waits up to 10 minutes after `docker compose up -d --build` returns.

`docker-compose.yml` publishes host ports `80`, `443`, `5432`, `8080`, and `8081`, and creates a network named `traefik`. Two stacks cannot run side by side. Launch exits `1` when any of those ports is listening, when the `traefik` network already exists, or when project `goforge-verify` already has containers. Do not drive an instance this run did not start.

A fresh volume has zero users. Registration is open because `GOFORGE_ENABLE_REGISTRATION` defaults to true in `internal/config/config.go`. There is no admin role. The first account is the bootstrap account.

## Doctor

```bash
.cursor/skills/verify-goforge/scripts/verify.sh doctor
```

Doctor is read-only. It is worth driving when all of these hold:

- Project `goforge-verify` has a running `goforge` container.
- `GET /health` returns `200` and `"status":"healthy"`.
- `GET /health/ready` returns `"database":"ok"`.
- `docker compose port goforge 8080` belongs to that project.

Doctor also records the `docker` check and `docker compose port db 5432`. On this tree the db service publishes `5432`. A mapping is expected. Deploy and service drives need `"docker":"ok"` as well. Auth drives need only the database check. Exit `1` when the instance is not worth driving.

## Drive

Run doctor, then one feature file. Cookies and tokens stay in `/tmp/goforge-verify`. Never print `stack.env`, cookie jars, `goforge_csrf`, `goforge_session`, or passwords.

Stable handles:

- CSRF cookie `goforge_csrf`, form field `csrf_token`, header `X-CSRF-Token`. State-changing routes reject a missing token with `403` and `invalid CSRF token`.
- Session cookie `goforge_session`.
- Register form `POST /auth/register` with `email`, `password`, `confirm_password`. The heading is `Create your account`. The submit button is `Register`.
- Login form `POST /auth/login`. The heading is `Sign in to GoForge`.
- After register or login the browser lands on `/dashboard`. The heading is `Dashboard`.
- New project form `POST /projects` with `name`, `repository_url`, `default_branch`, `dockerfile_path`, `build_context`. The manual URL field is renamed to `repository_url` by page script. HTTP drives post `repository_url` directly.
- Deploy control is the `Deploy Now` button, `POST /projects/{id}/deployments?env_id={env}`.
- Cancel control is the `Cancel` button, `POST /projects/{id}/deployments/{deploymentID}/cancel`.
- Log pane id `deployment-logs`. Live stream is `GET /projects/{id}/deployments/{deploymentID}/logs` with `Content-Type: text/event-stream`.
- Container stop id `stop-btn`, `POST /containers/{dockerID}/stop`. Restart id `restart-btn`, `POST /containers/{dockerID}/restart`.
- Service catalog link `Deploy` goes to `/services/{template}/configure`. Postgres posts to `POST /services/postgres`.

The register journey is `verify.sh drive-register`. The other feature files give the curl and SQL for that journey. Rate limit is 30 requests per second with burst 60.

## Evidence

Write proof under `artifacts/verify-goforge-proof/` at the repo root. Capture the action and the resulting state.

- `launch.log` is the Compose up output.
- `doctor.txt` is the doctor report.
- `revision.txt` is `git rev-parse HEAD` at launch.
- One verdict file per drive, for example `register-verdict.txt`.
- UI proof records HTTP status, the `Location` path, and whether a heading is present. Do not save HTML. Pages embed the CSRF token.
- Mutation proof is a second view. For register, that view is `select count(*) from users` plus `GET /dashboard` with the session cookie.
- For services, evidence is `sealed_marker` and `plaintext_password_present` booleans from `verify.sh check-sealed`. Do not copy `config` or `connection_info` into the evidence directory.
- Mocks are out. The drive uses the Compose stack, the real form routes, and the real Postgres in the `db` service.

`verify.sh` scrubs evidence text for `DATABASE_URL`, secret assignments, and cookie values before it returns.

## Cleanup

```bash
.cursor/skills/verify-goforge/scripts/verify.sh cleanup
```

Cleanup runs `docker compose -p goforge-verify down -v --remove-orphans` for the env file this run wrote, then deletes `/tmp/goforge-verify`. It does not delete `artifacts/verify-goforge-proof/`. It compares the evidence file list before and after teardown and exits `1` if a file disappeared. It removes the `traefik` network only when that network has zero attached containers. It never kills by process name.

Run cleanup after every failed launch too, so ports and the `traefik` network do not stick.

## Helpers

The helper is `.cursor/skills/verify-goforge/scripts/verify.sh`.

```bash
.cursor/skills/verify-goforge/scripts/verify.sh launch
.cursor/skills/verify-goforge/scripts/verify.sh doctor
.cursor/skills/verify-goforge/scripts/verify.sh drive-register
.cursor/skills/verify-goforge/scripts/verify.sh psql "select count(*) from users"
.cursor/skills/verify-goforge/scripts/verify.sh check-sealed verify-pg
.cursor/skills/verify-goforge/scripts/verify.sh cleanup
```

`psql` rejects SQL that mentions `password`, `secret`, `token`, `connection_info`, or `config`. `check-sealed` is the only command that reads those columns. It prints two booleans and deletes the row dump.

`drive-register` exits `0` when the harness finished and wrote `artifacts/verify-goforge-proof/register-verdict.txt`. A product miss such as registration staying open is `register-closed=fail` inside that file, not a harness failure. Exit `2` when the account never reaches `/dashboard`. Exit `1` when launch or doctor is refused.

## Maintenance

When the app changes, run `/maintain-verification-skill` against `.cursor/skills/verify-goforge/`. That pass re-reads each feature file and drives the live stack. It does not edit product code.
