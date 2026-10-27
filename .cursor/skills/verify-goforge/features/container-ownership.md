# Container stop and restart ownership

Stop and restart act on a container only for the account that owns it. A second account is denied on the detail page, the log stream, the stats stream, stop, and restart.

## Sub-features

- `container-owner-stop` stops a service container for the owning account.
- `container-owner-restart` starts it again for that same account.
- `container-other-denied` returns `403` for the other account on detail, logs, stats, stop, and restart.

## How to get to it (user POV)

- Open `Containers` at `/containers`. The list loads rows from `/containers/api`.
- Choose the container name to open `/containers/{dockerID}`.
- Choose the button with id `stop-btn`, or the button with id `restart-btn`.
- The same stop and restart actions appear on the container row in the list.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes` and `drive_docker=yes`.
- The bootstrap account in `jar-a.txt` owns a running one-click service. Follow [service-credentials.md](./service-credentials.md) through `service-create` first. The sealed-credential assertion can fail without blocking this journey.
- A second account exists. `POST /auth/register` as `verify-other@example.test` into `/tmp/goforge-verify/jar-b.txt` using the same CSRF flow as register. If registration is closed, set `GOFORGE_ENABLE_REGISTRATION=true` in a fresh launch and say so in the verdict. Do not seed a password hash by hand.
- The Docker id comes from `verify.sh psql "select docker_id from containers c join service_instances s on s.id = c.service_instance_id where s.name = 'verify-pg'"`.

- **Owner stop.** With `jar-a.txt` and header `X-CSRF-Token`, `POST /containers/$DOCKER_ID/stop`. The status is `200` and the response header `HX-Trigger` is `containerStopped`.
- **Owner restart.** `POST /containers/$DOCKER_ID/restart` with the same jar and header. The status is `200` and `HX-Trigger` is `containerRestarted`.
- **Other account.** With `jar-b.txt`, `GET /containers/$DOCKER_ID`, `GET /containers/$DOCKER_ID/logs`, and `GET /containers/$DOCKER_ID/stats` each return `403`. `POST` stop and `POST` restart each return `403` and the body contains `access denied`.
- **Proof.** Write the five statuses for account B and the two statuses for account A to `artifacts/verify-goforge-proof/container-verdict.txt`. Do not save response HTML.

## Gotchas

- The URL id is the Docker id, not the `containers.id` UUID.
- Stop and restart call `verifyContainerOwnership`, which allows the user only when `service_instance_id` points at that user's service. A deployment container is denied even for the project owner, because the handler does not follow `deployment_id`. Use the service container for the owner-success checks. Record a separate fail if you also probe a deployment container and the owner receives `403`.
- `Detail` does not call `verifyContainerOwnership`. On this tree, account B can receive `200` for `GET /containers/$DOCKER_ID`. `container-other-denied` still expects `403` on that GET. Record the miss. Do not drop the detail check.
- Log and stats routes do check ownership. Their `403` is not a substitute for the detail check.
- An id that is not in `containers` is `403` for stop and restart, including the owner. That is not an ownership pass.
- `hx-confirm` is browser-only. curl does not need to answer it. curl does need the CSRF header.
