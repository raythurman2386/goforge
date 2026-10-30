# Nightly S3 backups

With backups configured, `goforge serve` uploads a backup every night at the schedule (default `02:00 America/Chicago`) and `goforge backup run` uploads one on demand. A run writes the GoForge database, each running Postgres service (one dump per database plus `roles.sql`), and Gitea and Vaultwarden data under `<prefix><date>/<run id>/`, then `manifest.json`. Each run is a `backup_runs` row. Admins see the last run on Settings → Profile, through `GET /api/v1/backups`, and through the MCP tool `get_backup_status`. Retention keeps the newest `BACKUP_RETENTION_DAILY` complete days and prunes only after a successful run.

Configuration lives in the `backup_settings` row (migration 020) that an admin edits on Settings → Profile → Backups. Once the row exists it wins over `BACKUP_*`; with no row and `BACKUP_S3_BUCKET` set, serve copies `BACKUP_*` into it on start. A save applies live through `backup.Manager` (no restart); a run in progress keeps its config and the advisory lock.

## Sub-features

- `backup-disabled` shows "Backups are off" on Settings → Profile and `"enabled": false` from `GET /api/v1/backups` while the bucket is unset. Startup is not blocked.
- `backup-run` uploads every artifact and `manifest.json`. Each manifest entry's `sha256` and `bytes` match the object. The `backup_runs` row is `succeeded`.
- `backup-skip` records a stopped instance ("container is not running") and a template with no v1 target, such as redis, in `manifest.json` `skipped`, and the run still succeeds.
- `backup-failure` leaves a `failed` row with a one-line, redacted `error_summary`, writes no manifest, and prunes nothing.
- `backup-retention` deletes dates older than the newest N complete dates, and superseded runs within a kept date. Keys that do not match `<date>/<run id>/...` are never touched.
- `backup-secrets` never puts `ENCRYPTION_KEY` or the S3 keys in an object, a log line, or `error_summary`.
- `backup-settings-admin-edit`: an admin saves the form, gets `200` and the re-rendered card with the new schedule, and the row exists.
- `backup-settings-nonadmin-403`: a non-admin sees no backup card, and `PUT /settings/backups` and `POST /settings/backups/test` answer `403`.
- `backup-settings-secret-masked`: the secret is never in a response, the access key ID shows at most its last 4 characters, the Profile response is `Cache-Control: no-store`, both keys are sealed in the row (no plaintext), and neither appears in the app log or the audit row.
- `backup-settings-blank-keeps`: saving with blank key fields keeps the stored keys (Test connection still succeeds).
- `backup-settings-csrf-403`: a save without the CSRF token is `403` and changes nothing.
- `backup-settings-live-apply`: a save logs `next backup scheduled` for the new schedule with no restart; turning Enabled off logs `backups disabled` and keeps the other settings.
- `backup-settings-test-connection`: Test connection returns `200` against a reachable bucket, and `502` with no secret in the body for a wrong secret.
- `backup-settings-unreadable-keys`: when the saved keys do not decrypt with the current `ENCRYPTION_KEY`, backups stay off (no fallback to `BACKUP_*`), the form shows the non-secret settings and asks for both keys, a save with fewer than two keys is `400`, and a save with both turns backups back on with no restart. Unit-tested in `internal/backup/settings_test.go` (`TestUnreadableKeys_*`) and the repository test.
- `backup-env-ignored`: with saved settings present, an incomplete `BACKUP_*` (for example bucket set, keys removed) does not stop serve; it logs `BACKUP_* in the environment is invalid and ignored`. With no saved settings the same env still stops startup. Unit-tested as `TestCheckEnv`.
- `backup-status` returns `403` to a non-admin token on `GET /api/v1/backups`, and Settings shows no backup card to a non-admin.

## How to get to it (user POV)

- As the admin, open Settings → Profile → Backups, fill in the destination and keys (path-style on for MinIO), turn Enabled on, press Test connection, then Save. No restart.
- Or set `BACKUP_S3_ENDPOINT`, `BACKUP_S3_BUCKET`, `BACKUP_S3_ACCESS_KEY_ID`, `BACKUP_S3_SECRET_ACCESS_KEY` in your env file (`.env`) before the first start; serve copies them into the settings row. After that, env changes are ignored with a warning.
- Run `goforge backup run` in the app container, or wait for the schedule.
- Open Settings → Profile as the admin, or call `GET /api/v1/backups` with a read token.

## Driving it with verify.sh

`verify.sh drive-backup-settings` drives the settings form after `drive-register` and `drive-login`. It starts its own MinIO (`gfbk-verify-minio`, `cgr.dev/chainguard/minio`, bucket `gfbk-verify`, random keys kept in `$STATE_DIR` files, never on argv), then checks every `backup-settings-*` sub-feature above and writes `$EVIDENCE/backup-settings-verdict.txt`. `nonadmin-403` needs `GOFORGE_VERIFY_ENABLE_REGISTRATION=true` so it can register a second account. `cleanup` removes the MinIO container.

The drive never starts a backup run. It works against the compose stack from `launch`, or against any GoForge you point it at with:

- `GOFORGE_VERIFY_BASE` (default `http://127.0.0.1:8080`). With a non-default base, MinIO is published on a random `127.0.0.1` port instead of joining the compose network.
- `GOFORGE_VERIFY_PSQL`: the psql command for that stack's database (`-c <sql>` is appended).
- `GOFORGE_VERIFY_APP_LOGS`: a command that prints the app log.

Host mode, used when `launch` refuses because port 8080 is taken (build the branch, use a throwaway Postgres, never a live one):

```bash
go build -o /tmp/gfbk-goforge ./cmd/goforge
# serve with GOFORGE_PORT=18080, GOFORGE_BASE_URL=http://127.0.0.1:18080, DATABASE_URL at the
# throwaway Postgres, random ENCRYPTION_KEY/SESSION_SECRET/CSRF_SECRET/WEBHOOK_SECRET,
# GOFORGE_ENABLE_REGISTRATION=true, GOFORGE_ADMIN_EMAIL=verify-admin@example.test,
# LOG_FORMAT=json, no BACKUP_* vars; stdout to /tmp/goforge-verify/host-app.log
export GOFORGE_VERIFY_BASE=http://127.0.0.1:18080 GOFORGE_VERIFY_ENABLE_REGISTRATION=true \
  GOFORGE_VERIFY_PSQL="docker exec -i gfbk-test-pg psql -U goforge -d goforge_verify -v ON_ERROR_STOP=1 -tA" \
  GOFORGE_VERIFY_APP_LOGS="cat /tmp/goforge-verify/host-app.log"
verify.sh drive-register && verify.sh drive-login && verify.sh drive-backup-settings
```

With registration open, `register-closed`, `register-closed-post`, and `register-admin-role` in `register-verdict.txt` read `fail`. That is expected there.

The run smoke is the integration test, which starts its own throwaway containers (names `gfbk-it-*`): MinIO (`cgr.dev/chainguard/minio`), `postgres:17-alpine`, `gitea/gitea:1.25.5`, `vaultwarden/server:latest`, and volumes for each. It removes them when it ends. The restore test (`TestIntegration_Restore`, names `gfrs-it-*`) also uses `postgres:15-alpine`; see [backup-restore](./backup-restore.md).

Preconditions:

- Docker, `pg_restore`, and `sqlite3` on the host.
- `DATABASE_URL` pointing at a **throwaway** Postgres. The test runs migrations there and dumps it. Never point it at a live database.

```bash
docker run -d --name gfbk-test-pg -e POSTGRES_USER=goforge -e POSTGRES_PASSWORD=goforge \
  -e POSTGRES_DB=goforge -p 127.0.0.1:55432:5432 postgres:17-alpine
export DATABASE_URL='postgres://goforge:goforge@127.0.0.1:55432/goforge?sslmode=disable'
go test -tags integration -run Integration -count=1 -v ./internal/backup/
docker rm -f -v gfbk-test-pg
```

It covers `backup-run`, `backup-skip`, `backup-retention`, `backup-secrets`, and the Test connection probe (prefix found, empty prefix, wrong secret fails without leaking it): it restores nothing, but runs `pg_restore --list` on each dump, checks `roles.sql`, runs `PRAGMA integrity_check` on both SQLite snapshots, lists the tarballs, scans every object for the test `ENCRYPTION_KEY` and S3 secret, and checks the Vaultwarden and Gitea temp files are gone. `backup-failure` is covered by `internal/backup` unit tests, the `403` in `backup-status` by `internal/apiv1`, and the Settings lines by `internal/web/templates/pages/settings`. The non-admin Settings case is `backup-settings-nonadmin-403` in the drive. Settings precedence, auto-import, blank-keeps, and live apply are also unit-tested in `internal/backup` (`settings_test.go`, `manager_test.go`), the handlers in `internal/web/handlers/backup_settings_test.go`, and the sealed row in `internal/database/repositories/backup_settings_test.go` (needs `DATABASE_URL`).

Proof: keep the `go test -v` output in `/tmp/goforge-verify/proof/backups-integration.log`, and `backup-settings-verdict.txt` from the drive.

## Gotchas

- On minis the live `goforge-goforge-1` holds `127.0.0.1:8080`, so `launch` refuses. Do not stop it; use host mode above.
- Do not name a file `$STATE_DIR/app.log`: an earlier drive version scanned the log through that path and truncated a host log written there. The drive now uses a `mktemp` file and fails if the captured log is empty.
- Docker needs root on minis (`sudo -n docker`): `--env-file` works through sudo, `<(...)` process substitution and inherited `-e VAR` do not.
- `docker.io/minio/minio` and `quay.io/minio/minio` did not pull on minis on 2026-10-29. The test uses `cgr.dev/chainguard/minio`.
- If the Docker socket needs root, run the test with `sudo`, then give the Go caches back (`chown -R`).
- Do not probe a starting Gitea with `sqlite3 /data/gitea/gitea.db` as root. sqlite3 creates a missing file, and Gitea then fails with "attempt to write a readonly database".
- Never run `goforge backup run` against live Gitea, Vaultwarden, or shared Postgres containers as a test. The live path has not been driven.
- A run holds a Postgres advisory lock. A second run at the same time fails to start with "another backup run is in progress".
