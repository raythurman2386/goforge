# Nightly S3 backups

With `BACKUP_S3_BUCKET` set, `goforge serve` uploads a backup every night at `BACKUP_SCHEDULE` (default `02:00 America/Chicago`) and `goforge backup run` uploads one on demand. A run writes the GoForge database, each running Postgres service (one dump per database plus `roles.sql`), and Gitea and Vaultwarden data under `<prefix><date>/<run id>/`, then `manifest.json`. Each run is a `backup_runs` row. Admins see the last run on Settings → Profile, through `GET /api/v1/backups`, and through the MCP tool `get_backup_status`. Retention keeps the newest `BACKUP_RETENTION_DAILY` complete days and prunes only after a successful run.

## Sub-features

- `backup-disabled` shows "Backups are off" on Settings → Profile and `"enabled": false` from `GET /api/v1/backups` while the bucket is unset. Startup is not blocked.
- `backup-run` uploads every artifact and `manifest.json`. Each manifest entry's `sha256` and `bytes` match the object. The `backup_runs` row is `succeeded`.
- `backup-skip` records a stopped instance ("container is not running") and a template with no v1 target, such as redis, in `manifest.json` `skipped`, and the run still succeeds.
- `backup-failure` leaves a `failed` row with a one-line, redacted `error_summary`, writes no manifest, and prunes nothing.
- `backup-retention` deletes dates older than the newest N complete dates, and superseded runs within a kept date. Keys that do not match `<date>/<run id>/...` are never touched.
- `backup-secrets` never puts `ENCRYPTION_KEY` or the S3 keys in an object, a log line, or `error_summary`.
- `backup-status` returns `403` to a non-admin token on `GET /api/v1/backups`, and Settings shows no backup card to a non-admin.

## How to get to it (user POV)

- Set `BACKUP_S3_ENDPOINT`, `BACKUP_S3_BUCKET`, `BACKUP_S3_ACCESS_KEY_ID`, `BACKUP_S3_SECRET_ACCESS_KEY` (and `BACKUP_S3_FORCE_PATH_STYLE=true` for MinIO), then restart.
- Run `goforge backup run` in the app container, or wait for the schedule.
- Open Settings → Profile as the admin, or call `GET /api/v1/backups` with a read token.

## Driving it with verify.sh

There is no `verify.sh` drive yet. The maintained smoke is the integration test, which starts its own throwaway containers (names `gfbk-it-*`): MinIO (`cgr.dev/chainguard/minio`), `postgres:17-alpine`, `gitea/gitea:1.25.5`, `vaultwarden/server:latest`, and volumes for each. It removes them when it ends.

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

It covers `backup-run`, `backup-skip`, `backup-retention`, and `backup-secrets`: it restores nothing, but runs `pg_restore --list` on each dump, checks `roles.sql`, runs `PRAGMA integrity_check` on both SQLite snapshots, lists the tarballs, scans every object for the test `ENCRYPTION_KEY` and S3 secret, and checks the Vaultwarden and Gitea temp files are gone. `backup-failure` is covered by `internal/backup` unit tests, the `403` in `backup-status` by `internal/apiv1`, and the Settings lines by `internal/web/templates/pages/settings`. The non-admin Settings case has no test yet.

Proof: keep the `go test -v` output in `/tmp/goforge-verify/proof/backups-integration.log`.

## Gotchas

- `docker.io/minio/minio` and `quay.io/minio/minio` did not pull on minis on 2026-10-29. The test uses `cgr.dev/chainguard/minio`.
- If the Docker socket needs root, run the test with `sudo`, then give the Go caches back (`chown -R`).
- Do not probe a starting Gitea with `sqlite3 /data/gitea/gitea.db` as root. sqlite3 creates a missing file, and Gitea then fails with "attempt to write a readonly database".
- Never run `goforge backup run` against live Gitea, Vaultwarden, or shared Postgres containers as a test. The live path has not been driven.
- A run holds a Postgres advisory lock. A second run at the same time fails to start with "another backup run is in progress".
