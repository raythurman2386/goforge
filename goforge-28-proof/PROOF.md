# PR #28 live proof

Code under test: `06485da` on `cursor/usable-deploy-loop-9102`.
Compose project: `gf28proof`. Stack torn down. App containers created through the Docker socket were removed after `docker compose down -v`.

`make check` passed locally after the Go 1.25 toolchain's missing `covdata` tool was built into that toolchain. GitHub CI on this commit was already green before these artifacts were added.

## Scorecard

| Item | Result | Evidence |
| --- | --- | --- |
| `make check` | Pass | `01-make-check.log` ends with `All checks passed!` and `EXIT:0` |
| Compose refuses an empty `ENCRYPTION_KEY` | Pass | `02-compose-missing-secret.log` (`GATE_PASS`) |
| Compose comes up | Pass | `03-compose-up.log`, `03-compose-ps.txt` |
| Postgres is not published | Pass | `04-db-port.txt`: container `Ports` is `{"5432/tcp":null}`. `docker compose port` printed `:0`. Host port 5432 is the separate unit-test Postgres, not the Compose database. |
| Production process refuses a blank key | Pass | `05-no-key.log`: `ENCRYPTION_KEY is required` |
| First account is admin and registration closes | Pass | `06-registration.log`, `06-roles.txt` |
| Later account is a normal user | Pass | `06-roles.txt`: second email has role `user` |
| Stored logs and SSE replay | Pass | `07-db-logs.txt` (38 rows), `07-sse-summary.txt` (marker once on first connect and once on reconnect), `07-detail.html` |
| Live status | Pass | `09-sse-status.txt` contains `event: status` and a Canceled badge. Browser `browser/03-canceled-deployment.png` |
| Deployment detail route | Pass | HTTP page plus `browser/03-canceled-deployment.png` and `browser/05-hello-deployment.png` |
| Refresh keeps stored logs | Pass | `07-detail.html` contains `goforge-proof-hello-build`. Browser reload `browser/04-canceled-reload.png` |
| Container ownership | Pass | `08-ownership.log`: user B is 403 on detail, logs, stats, stop, and restart. User A is 200. |
| Admin prune gate | Pass | `08-ownership.log`: user B is 403 on `/dashboard/prune` and `/settings/prune`. Admin dashboard prune returned 303 and settings prune returned 200. Dashboard shows Prune System in `browser/02-dashboard.png`. |
| Cancel stays canceled and the build stops | Pass | `09-cancel.log`: status `canceled` / `canceled by user` across the 90s poll. `sleep 60` was running before cancel and gone after. |
| Migration 015 rewrites an old `cancelled` row | Pass | `09-migrate-015.log`: `cancelled` then `canceled` then `cancelled` on down. `mig_exit:0` |
| Service secrets at rest, plaintext in the UI | Pass | `10-service-enc.txt` is only `_goforge_enc` and has no `postgres://` URL. `10-service-ui.html` shows the connection field (URL redacted in the artifact). Browser confirmed Connection Details. The screenshot was not kept because it showed the generated password. |
| `dockerfile_path` and `build_context` | Pass | `11-dockerfile-path.log`: project stored `docker/Dockerfile.prod` and `app`. Build ran `COPY hello.txt` from that context and reached `running`. |
| Teardown | Pass | `12-down.log` (`down_exit:0`). No `goforge` containers, volumes, or proof networks left. |

## Commands

1. `make check`
2. `docker compose --env-file .env.empty config` with `ENCRYPTION_KEY` empty
3. `COMPOSE_PROJECT_NAME=gf28proof docker compose up -d --build`
4. `docker compose port db 5432` and `docker inspect` published ports
5. `docker compose run --rm --no-deps -e ENCRYPTION_KEY= -e GOFORGE_DEV_MODE=false goforge`
6. `GET /auth/register`, register the first account, `GET /auth/register` again. Enable registration, register a second account, `SELECT email, role FROM users`.
7. Deploy `git://172.19.0.1/hello.git`, read `deployment_logs`, curl the logs SSE twice, GET the detail page.
8. With the second account, call the first deployment's container routes. With the admin account, call the same routes and both prune endpoints.
9. Deploy `git://172.19.0.1/sleep.git` (`RUN sleep 60`), POST cancel, poll `deployments.status` every 5s. Separately, migrate a database to 014, restore the old `cancelled` check, insert a row, step to 015, step down.
10. `POST /services/postgres`, then `SELECT config, connection_info FROM service_instances` and GET the service page.
11. Deploy `dockerfile_path=docker/Dockerfile.prod` and `build_context=app`.
12. `docker compose down -v`, then remove the containers the app had started on the host daemon.

Git repos were served by host `git daemon` on port 9418. The Compose network could reach the host at `172.19.0.1` after the VM's leftover iptables-legacy `FORWARD` policy was set to `ACCEPT`. That was an environment fix, not a product change.

## Browser

Signed in as the admin and opened login, dashboard, the canceled deployment, a reload of that page, the hello deployment, the Postgres service, and the hello container. Screenshots are in `browser/`. The service screenshot was deleted because the page shows the generated password.

## Soft gaps

- `GET /register` is 404. The form is `GET /auth/register` (200, then 403 after the first account).
- User A's container log and stats curls returned 200 and then hit the SSE `--max-time`, so the shell log also says `curl_fail`. The status code is 200 and the browser container page rendered.
- Rows that were stored in plaintext before encryption still reseal on the next write. This run only created a new service.
- `openJSON` still returns the sealed blob when decryption fails.
- CI does not build the Docker image. The image build in this proof is the Compose build plus the app's own image builds.
- No verification skill was created.
