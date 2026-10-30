# Service and project metrics after a config save

After a user saves `Edit configuration` (or runs `Update image`) on a running service, the service page polls while the instance is pending, stops polling once it is `running`, and the `Resource Usage` card shows live CPU, memory, network I/O, and block I/O without a reload. A stream for a container that is gone or stopped says so instead of spinning on `Connecting to metrics stream...`. Service detail and the project Overview also show a `Container` card (image, state, health, uptime, restarts, OOM flag, limits, ports, Traefik host rule) with a `View logs` link to `/containers/{dockerID}`. The project Overview only ever streams a running container: the latest deployment's, else the newest earlier `running` deployment's, else an empty state.

## Sub-features

- `metrics-edit-save` saves the edit form: `POST /services/{instanceID}/edit` answers `303` to `/services/{instanceID}` and the instance goes `pending`.
- `metrics-poll-stops` the pending render of `GET /services/{instanceID}` carries `id="service-detail-poll"` inside `id="service-detail-content"`; the first `running` render has no `service-detail-poll`, so htmx stops polling after that swap.
- `metrics-stream-first-frame` `GET /containers/{dockerID}/stats` sends `event: connected` and then `event: stats` within about 1 second, with `Network I/O` and `Block I/O` in the frame.
- `metrics-stream-unavailable` the same route for a stopped or removed container sends `event: unavailable` with `Metrics are not available: ...` and no `event: stats`.
- `metrics-stream-preserved` the stream element is `id="metrics-stream-{dockerID}"` with `hx-preserve="true"`, so a pending poll swap keeps it open while the Docker ID is unchanged; a recreate renders a new id.
- `metrics-container-card` service detail and project Overview render `data-container-card` with the fields above and `href="/containers/{dockerID}"`.
- `metrics-project-empty` a project whose environment has no running container renders `No running container for this environment.` and no `sse-connect`.

## How to get to it (user POV)

- Sidebar `Services`, open the instance from `Your Services` (`/services/{instanceID}`).
- Choose `Edit configuration` (`/services/{instanceID}/edit`), change a non-immutable field, submit the form.
- The page returns to `/services/{instanceID}` with `Deploying...`, then the badge flips to `Running`, and `Resource Usage` fills in.
- `Update image` on the same page (`POST /services/{instanceID}/update`, `HX-Redirect`) takes the same pending path.
- Project page `/projects/{id}` Overview tab: `Resource Usage` and `Container` cards below `Latest Deployment`.
- `View logs` on the `Container` card opens `/containers/{dockerID}`, whose `container-stats` panel uses the same stream.

## Driving it with verify.sh

Preconditions:

- Throwaway stack only. On minis the live `goforge-goforge-1` holds `127.0.0.1:8080`: use host mode from SKILL.md with `GOFORGE_PORT=18080` and prefix `GOFORGE_VERIFY_DOCKER="sudo -n docker"`. Never point this drive at `127.0.0.1:8080` or any live `goforge-*` container, and never at gitea, vaultwarden, shared-pg, shared-redis, or traefik.
- `verify.sh doctor` reports `drive_auth=yes` and `drive_docker=yes`. Stats need Docker access from the serving process; host mode without Docker access can only drive `metrics-stream-unavailable` and the template checks.
- A running `verify-pg` from [service-credentials.md](./service-credentials.md) through `service-create`, with status `running`.
- `BASE=http://127.0.0.1:18080` in host mode. `INSTANCE_ID` from `verify.sh psql "select id from service_instances where name = 'verify-pg'"`.

- **Edit save.** `GET $BASE/services/$INSTANCE_ID/edit` with `jar-a.txt` returns `200`. Post the form back with `csrf_token`, `name=verify-pg`, and the unchanged non-secret fields (`database=app`, `username=postgres`; leave `password` empty to keep it). Expect `303` with `Location: /services/$INSTANCE_ID`.
- **Pending render.** Right after the save, `GET $BASE/services/$INSTANCE_ID`. Record `pending_has_poll` (body contains `id="service-detail-poll"`). Save nothing else from the body; it contains masked connection rows only, but keep the habit.
- **Running render.** Poll `verify.sh psql "select status from service_instances where name = 'verify-pg'"` until `running`, then `GET` the page again with header `HX-Request: true`. Record `running_has_poll` (must be `no`), `has_stream_id` (`id="metrics-stream-$DOCKER_ID"`), and `has_container_card` (`data-container-card`).
- **First frame.** `DOCKER_ID` from `verify.sh psql "select docker_id from containers c join service_instances s on s.id = c.service_instance_id where s.name = 'verify-pg'"`. Run `curl -sS -N --max-time 2 -b jar-a.txt "$BASE/containers/$DOCKER_ID/stats" -o /tmp/goforge-verify/state/stats.txt -w '%{http_code}\n'`. Expect exit `28` (timeout) with code `200`; `grep -c '^event: stats' stats.txt` is at least `1` and the file contains `Network I/O`.
- **Unavailable.** `sudo -n docker stop $DOCKER_ID` (the throwaway service only), then the same curl. The file contains `event: unavailable` and `the container is not running`, and no `event: stats`. Start it again with `POST $BASE/containers/$DOCKER_ID/restart`.
- **Project empty state.** Optional: a project with a deployment and no running container renders `No running container for this environment.` on `/projects/{id}`.
- **Proof.** Write the statuses and booleans above to `/tmp/goforge-verify/proof/metrics-verdict.txt`. Do not copy jars or full page HTML.

## Gotchas

- Unit coverage for every sub-feature: `go test -run 'TestDetail' ./internal/web/templates/pages/services/`, `go test -run 'TestOverview' ./internal/web/templates/pages/projects/`, `go test -run 'TestContainerStats|TestActiveContainer' ./internal/web/handlers/`, `go test -run 'TestMetricsFromSamples' ./internal/monitor/`, `go test -run TestSummarizeInspect ./internal/docker/`.
- `hx-preserve` and the poll stopping are browser behavior. curl proves the markup (poll inside the swapped region, absent once running; stream id keyed by Docker ID), not the htmx swap itself.
- The first frame waits for two one-shot samples about 500 ms apart so CPU is a real delta. Later frames come every 2 seconds. Network and block I/O are totals since the container started, not rates.
- `unavailable` is sent once per reason; the stream keeps sampling and goes back to `stats` when the container runs again.
- During a recreate the page can still render the old Docker ID until `finishServiceContainer` deletes the old row. Once deleted, `/containers/{old}/stats` is `403` (ownership), not `unavailable`; the next running render carries the new id.
- The `Container` card reads Docker inspect through `docker.SummarizeInspect`, the same mapping `/api/v1` uses for `container.docker`. Env, command, and args are never read. Volumes and networks are in the API but not on the card.
