# Container stop and restart ownership

Stop and restart act on a container only for the account that owns it. A second account is denied with `403` on the detail page, the log stream, the stats stream, stop, and restart. Ownership follows either the service instance or the deployment's project.

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
- The bootstrap account in `/tmp/goforge-verify/state/jar-a.txt` owns a running one-click service. Follow [service-credentials.md](./service-credentials.md) through `service-create` first. The sealed-credential assertion can fail without blocking this journey.
- A second account exists. Registration is closed after the first account by default, so launch with `GOFORGE_VERIFY_ENABLE_REGISTRATION=true` and say so in the verdict. `POST /auth/register` as `verify-other@example.test` into `/tmp/goforge-verify/state/jar-b.txt` using the same CSRF flow as register. Do not seed a password hash by hand.
- The Docker id comes from `verify.sh psql "select docker_id from containers c join service_instances s on s.id = c.service_instance_id where s.name = 'verify-pg'"`.

- **Owner stop.** With `jar-a.txt` and header `X-CSRF-Token`, `POST /containers/$DOCKER_ID/stop`. The status is `200` and the response header `HX-Trigger` is `containerStopped`.
- **Owner restart.** `POST /containers/$DOCKER_ID/restart` with the same jar and header. The status is `200` and `HX-Trigger` is `containerRestarted`.
- **Other account.** With `jar-b.txt`, `GET /containers/$DOCKER_ID`, `GET /containers/$DOCKER_ID/logs`, and `GET /containers/$DOCKER_ID/stats` each return `403`. `POST` stop and `POST` restart each return `403` and the body contains `access denied`.
- **Proof.** Write the five statuses for account B and the two statuses for account A to `/tmp/goforge-verify/proof/container-verdict.txt`. Do not save response HTML.

## Gotchas

- The URL id is the Docker id, not the `containers.id` UUID.
- Detail, stop, and restart call `verifyContainerOwnership`, which uses `containers.OwnedByUser`. It allows the user when `service_instance_id` points at that user's service, or when `deployment_id` leads to an environment in that user's project. A deployment container is allowed for its project owner.
- Containers stay `403` `access denied` for another account. Projects and deployments use `404` instead. Do not mix the two.
- Log and stats routes do check ownership. Their `403` is not a substitute for the detail check.
- An id that is not in `containers` is `403` for stop and restart, including the owner. That is not an ownership pass.
- `hx-confirm` is browser-only. curl does not need to answer it. curl does need the CSRF header.
