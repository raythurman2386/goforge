# Readiness check without error text

Anyone, signed in or not, can call `GET /health/ready`. It returns `200` with `"status":"ready"` when no configured check fails and `503` with `"status":"not_ready"` otherwise. The `checks` object always has the keys `database` and `docker`, and each value is only `ok`, `unavailable`, or `not_configured`. Database and Docker error text (hosts, socket paths, driver messages) never appears in the body; it goes to the server log as a `readiness check failed` warning with `check` and `error` fields (HEALTH-READY-PUBLIC, closed in v0.5.0 S1).

## Sub-features

- `ready-public` answers without a cookie or token. No redirect to `/login`.
- `ready-database-ok` reads `"database":"ok"` against a reachable database. `verify.sh launch` and `doctor` depend on this exact string.
- `ready-docker-unavailable` reads `"docker":"unavailable"` and `503` when the Docker daemon is unreachable, with no `docker.sock`, `unix://`, or `Cannot connect` anywhere in the body.
- `ready-values-closed` every value in `checks` is one of `ok`, `unavailable`, `not_configured`.
- `ready-error-logged` the app log has a `readiness check failed` line with `"check":"docker"` and the real error.

## How to get to it (user POV)

- `curl -s -i http://127.0.0.1:8080/health/ready` from the host (or `:18080` in host mode). There is no UI page.
- Monitoring tools and load balancers poll the same URL anonymously.

## Driving it with verify.sh

Compose mode: `verify.sh launch` waits for `"database":"ok"` and copies the body to `/tmp/goforge-verify/proof/ready.json`. `verify.sh doctor` writes `database=` and `docker=` to `/tmp/goforge-verify/proof/doctor.txt`.

Host mode (live stack holds `:8080`; see SKILL.md "Host mode"). Docker is not reachable from a non-root host serve, which gives the `unavailable` case for free:

- Start throwaway `postgres:17-alpine` and `goforge serve` on `127.0.0.1:18080` with `LOG_FORMAT=json` and stdout to `/tmp/goforge-verify/host-app.log`.
- **Body.** `curl -s -o /tmp/goforge-verify/proof/health-ready.json -w '%{http_code}' http://127.0.0.1:18080/health/ready` prints `503`, and the body has `"database":"ok"` and `"docker":"unavailable"`.
- **No leak.** `grep -E 'docker.sock|unix://|Cannot connect|permission denied|dial ' /tmp/goforge-verify/proof/health-ready.json` prints nothing.
- **Closed values.** `python3 -c 'import json,sys; c=json.load(open(sys.argv[1]))["checks"]; assert set(c)=={"database","docker"} and set(c.values())<={"ok","unavailable","not_configured"}, c' /tmp/goforge-verify/proof/health-ready.json` exits `0`.
- **Helper still parses it.** `GOFORGE_VERIFY_BASE=http://127.0.0.1:18080 GOFORGE_VERIFY_DOCKER="sudo -n docker" .cursor/skills/verify-goforge/scripts/verify.sh doctor` writes `database=ok` and `docker=unavailable` to `doctor.txt`. Doctor exits `1` in host mode because there is no `goforge-verify` Compose container; the `database=ok` line is the proof.
- **Logged.** `grep '"readiness check failed"' /tmp/goforge-verify/host-app.log` shows `"check":"docker"` with the Docker error.
- **Signed in is the same.** After `drive-register` or `drive-login` against the host serve, `curl -s -b <jar> .../health/ready` returns the same keys and values. There is no detail view for admins.
- **Proof.** `/tmp/goforge-verify/proof/health-ready.json`, `doctor.txt`, and a `health-ready-verdict.txt` listing each sub-feature as `pass` or `fail`.

Unit coverage: `internal/web/handlers/health_test.go` (`TestHealthReady_NoErrorTextInBodyAndErrorLogged` fakes a failing database and Docker with realistic error strings and asserts the body has none of them and the log has both) and `internal/web/router_test.go` `TestRouter_HealthReadyEndpoint` (anonymous `200` with `not_configured` values).

## Gotchas

- `not_configured` does not fail readiness. Only `unavailable` makes it `503`.
- `/health` is the liveness check and has no `checks`. Do not use it to prove this feature.
- Do not change the `"database":"ok"` string. `verify.sh` lines in `cmd_launch` and `cmd_doctor` match it exactly.
- Never curl or drive the live `goforge-goforge-1` on `:8080` for this; use a stack this run started.
