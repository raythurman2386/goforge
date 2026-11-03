# Dashboard Prune System feedback

An admin presses **Prune System** on the Dashboard. The prune itself is unchanged (stopped managed containers, unused managed images, then all unused build cache, engine-wide; see `docs/guides/monitoring.md#system-prune`). This file covers the feedback around it (t1905u).

## Sub-features

- `pruning-pending`: after the kit confirm dialog, the button is `disabled`, carries `htmx-request`, shows the spinner (`.htmx-indicator`), and reads `Pruning…` until the answer lands.
- `pruned-summary`: `POST /dashboard/prune` with `HX-Request: true` answers `200` with `#dashboard-stats` re-rendered (fresh disk numbers) and `components.Toast` out of band into `#toast`: `Pruned <bytes>: N containers, N images, N build cache records`, or `Nothing to free…` when Docker reclaimed 0 bytes. Never `Pruned 0 B`.
- `prune-error`: a failed prune answers `500` with the `RenderError` alert fragment. htmx does not swap 5xx, so the card stays and `app.js` shows the error toast `Prune System failed. Check the GoForge logs and try again.` The Docker error is logged as `system prune failed`, never sent to the browser.
- `no-reload`: the whole flow is one fetch. The document is not navigated (a `window` marker set before the click survives), the URL stays `/dashboard`, and Back from `/containers` lands on the Dashboard with the post-prune numbers and no stale toast.

A plain form POST (no `HX-Request`) still answers `303` to `/dashboard`. A `user` gets `403`.

## How to get to it (user POV)

1. Sign in as the admin and open **Dashboard** from the sidebar.
2. Under **Docker Disk Usage**, **Prune System** is enabled when **Reclaimable Space** is above zero; otherwise it is disabled with the title `Nothing to prune`.
3. Click it, read the engine-wide warning in the dialog, and press **Prune**.
4. Watch the button (`Pruning…` with a spinner), then the toast and the refreshed card.

## Driving it with verify.sh

**Never confirm Prune against an engine you care about.** Build cache prune is engine-wide. Drive it only against a throwaway `docker:27-dind` engine, in host mode ([SKILL.md](../SKILL.md#host-mode)):

```bash
# Record the host engine first; it must read the same after the run.
sudo -n docker system df > "$PROOF/minis-df-before.txt"
sudo -n docker run -d --privileged --name gf-prune-dind -p 127.0.0.1:23750:2375 -e DOCKER_TLS_CERTDIR= docker:27-dind
export DOCKER_HOST=tcp://127.0.0.1:23750   # every docker call below talks to the dind engine only
# Seed: a small build (build cache) and one stopped container with the managed label.
printf 'FROM alpine:3.20\nRUN head -c 8388608 /dev/urandom > /blob\n' | docker build -t goforge-prune-seed:1 -
docker run --name gf-prune-seed --label goforge.managed=true goforge-prune-seed:1 true
docker system df > "$PROOF/dind-df-before.txt"
```

Use `docker inspect` on a GoForge-created container, or `ManagedLabelFilter` in `internal/docker`, for the exact managed label. Then start a throwaway `postgres:17-alpine` and `goforge serve` on `127.0.0.1:18080` with `DOCKER_HOST=tcp://127.0.0.1:23750` in its mode `600` env file, run `verify.sh drive-register`, and drive the browser with the pinned `playwright-core` from `ui-a11y/` (`/tmp/goforge-verify/ui-a11y-node`):

- Log in, open `/projects`, then click the sidebar **Dashboard** link. Wait for `[data-testid="dashboard-prune"]`. Set `window.__gfNoReload = 1`.
- Click the button, then the dialog's **Prune**. While the request runs, the button is `:disabled`, has class `htmx-request`, and its visible text is `Pruning…` (`pruning-pending`). Slow the request with `page.route` (continue after a short delay) if the dind prune is too fast to catch.
- `#toast` has the `Pruned …` text, `#dashboard-stats` shows the new numbers, `window.__gfNoReload` is still `1`, and the URL is `/dashboard` (`pruned-summary`, `no-reload`).
- Click sidebar **Containers**, then `page.goBack()`. The Dashboard shows the post-prune card and no toast.
- Stop the dind container (`sudo -n docker stop gf-prune-dind`), reload, and prune again: an error toast appears, `#dashboard-stats` keeps its content, and the URL is `/dashboard` (`prune-error`). The button only renders enabled when stats loaded while the engine was up, so stop dind after the page has loaded, or seed again first.
- `docker system df` on the dind engine after the success step is the second view. `verify.sh drive-ui-a11y` against `18080` should keep `axe-gate=pass` on `/dashboard` in light and dark.

Cleanup: stop serve by its PID, `sudo -n docker rm -f -v gf-prune-dind <pg-container>`, and compare `sudo -n docker system df` on the host with `minis-df-before.txt`.

## Gotchas

- `/dashboard/prune` sits under the 15 s route timeout. A very large build cache prune could hit it and show the error toast even though Docker keeps pruning. Not changed by t1905u.
- The image count is entries in `ImagesDeleted` with `Deleted` set. Untag entries are not counted, so one image with two tags counts once.
- `#dashboard-stats` loads once on page load and does not poll, so the swapped card stays until the next navigation.
- Host mode without `DOCKER_HOST` has no Docker, so the disk card does not render and there is no button to press.
