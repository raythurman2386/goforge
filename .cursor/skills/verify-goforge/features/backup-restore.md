# Restore the GoForge database from a backup run

An operator lists the backup runs with `goforge backup list` and restores the GoForge database from one with `goforge backup restore <run key>`, with GoForge stopped. Only `app/goforge-db.dump` is restored: the dump loads into a side database, sessions are cleared, a `backup.restore` audit row is written, and the side database is renamed into place. The previous database is kept as `<db>_pre_restore_<UTC time>`. Admins also see a read-only Restore points list on Settings → Profile → Backups with the command to run. There is no restore in the web UI, the API, or MCP.

## Sub-features

- `restore-list`: `goforge backup list` prints complete runs newest first (run key, trigger, GoForge version, start, bytes, skipped) and counts runs with no manifest as incomplete.
- `restore-points-card`: Settings → Profile loads `GET /settings/backups/restore-points` (admin only, `Cache-Control: no-store`, `404` when not wired) showing the same runs and the stop / `docker compose run --rm --no-deps goforge ./goforge backup restore <key>` / start commands. No form, button, or `hx-post`/`hx-put`/`hx-delete`.
- `restore-dry-run`: without `--yes` it prints the plan (run, dump size and SHA-256, side database, rename) and the preflight result, and changes nothing. A failing preflight exits non-zero.
- `restore-confirm`: `--yes` asks for the run key on a TTY; with no TTY it needs `--confirm=<run key>`; a wrong key changes nothing.
- `restore-refuse-no-manifest`, `restore-refuse-manifest-too-new`, `restore-refuse-sha-mismatch`, `restore-refuse-bytes-mismatch`, `restore-refuse-lock-held` (a backup run holds the advisory lock), `restore-refuse-other-connections` (message ends `stop GoForge first: docker compose stop goforge`), `restore-refuse-schema-newer` (side DB `schema_migrations` newer than the binary), and `restore-refuse-broken-dump`: each leaves the live database and its data unchanged and no side database behind.
- `restore-side-db-swap`: the dump goes through `pg_restore -f -` (PostgreSQL 17 client) into `psql --single-transaction` with `ON_ERROR_STOP`, into `<db>_restore_<run id>`, then two `ALTER DATABASE ... RENAME` in one transaction.
- `restore-keeps-previous`: `<db>_pre_restore_<UTC time>` exists afterwards with the pre-restore data; nothing drops it. The command prints the undo SQL.
- `restore-sessions-audit`: `sessions` is empty, API tokens are as in the dump, and `audit_logs` has `backup.restore` / `backup_run` with only `{"run_key": ...}`.
- `restore-pg15-target`: works against a `postgres:15-alpine` server (the `SET transaction_timeout` line from pg_dump 17 is dropped).
- `restore-scope`: only `manifest.json` and `app/goforge-db.dump` are read from the bucket; no service container is touched.
- `restore-secrets`: no password, key, or `DATABASE_URL` on argv or in the output or logs.
- `restore-after-start`: the next `goforge serve` runs migrations, the old session cookie is sent to `/login`, the admin logs in again, and the data matches the backup.

## How to get to it (user POV)

```bash
docker compose exec goforge ./goforge backup list
docker compose stop goforge
docker compose run --rm --no-deps goforge ./goforge backup restore <run key>          # dry run
docker compose run --rm --no-deps goforge ./goforge backup restore <run key> --yes    # type the run key
docker compose start goforge
```

Or open Settings → Profile → Backups as the admin and read Restore points.

## Driving it with verify.sh

There is no `verify.sh drive-restore` yet. Two paths cover it.

Integration test (all refusals, the swap, PG15 and PG17). It starts its own MinIO (`cgr.dev/chainguard/minio`) and `postgres:15-alpine` and `postgres:17-alpine` (names `gfrs-it-*`) and removes them. It needs `pg_restore` and `psql` 17 on `PATH`; on minis the host clients are 18, so build the test binary and run it in an alpine container with `postgresql17-client` and `docker-cli`, host network, and the Docker socket mounted:

```bash
go tool templ generate
CGO_ENABLED=0 go test -c -tags integration -o /tmp/gfrs-runner/backup.test ./internal/backup/
# Dockerfile: FROM alpine:3.24; RUN apk add --no-cache postgresql17-client docker-cli tzdata
sudo -n docker build -t gfrs-it-runner:local /tmp/gfrs-runner
sudo -n docker run --rm --network host -v /var/run/docker.sock:/var/run/docker.sock \
  -v /tmp/gfrs-runner:/w -w /w gfrs-it-runner:local ./backup.test -test.run Integration_Restore -test.v
```

`GOFORGE_IT_PG_IMAGES` overrides the server images (comma separated). It covers every `restore-refuse-*`, `restore-side-db-swap`, `restore-keeps-previous`, `restore-sessions-audit`, `restore-pg15-target`, `restore-scope` (a recording store), `restore-secrets` (progress output scanned), and a second `MigrateUp`.

Host-mode drive (`restore-list`, `restore-points-card`, `restore-dry-run`, `restore-confirm`, `restore-refuse-other-connections`, `restore-after-start`), as in [backups](./backups.md) host mode: throwaway `postgres:15-alpine` and MinIO, serve on `127.0.0.1:18080` with `BACKUP_*` pointing at the MinIO, then

1. `verify.sh drive-register`, `goforge backup run`, `goforge backup list`.
2. `verify.sh drive-login` (creates `verify-smoke` after the backup), GET Profile and `/settings/backups/restore-points` with the admin jar.
3. `goforge backup restore <key>` while serve runs: refused, `stop GoForge first`.
4. Stop serve; dry run; `--yes` with stdin `/dev/null` (refused, no TTY); `--yes --confirm=<key>`.
5. SQL: databases, `projects` empty, `sessions` 0, audit row, `verify-smoke` in the pre-restore DB.
6. Start serve; old jar gets `303 /login`; fresh login `303 /dashboard`; `/projects` has no `verify-smoke`.

Run the CLI with the PostgreSQL 17 client (the runtime image, or the runner image above with the static binary mounted and `--env-file` for the env).

## Gotchas

- Never run either path against the live stack or the live bucket. Restore swaps whole databases.
- `docker compose exec -T db pg_restore` (the old runbook) fails: the `db` image is PostgreSQL 15 and cannot read a pg_dump 17 archive (`unsupported version (1.16) in file header`).
- Stop serve by the PID of the `goforge serve` process itself. A subshell's `$!` can be a wrapper, and the restore then rightly refuses with "other clients are connected".
- `/dev/null` is a character device; the TTY check uses `isatty`, so `docker run` without `-t` needs `--confirm`.
- Serve copies `BACKUP_*` into `backup_settings` on first start, so `backup list` says `settings: settings` even in an env-only setup.
- The `<db>_pre_restore_*` databases pile up; drop them by hand.
