# Restore a Postgres, Gitea, or Vaultwarden service from a backup run

An operator restores one Postgres, Gitea, or Vaultwarden service's data volume (`/var/lib/postgresql/data` or `/data`) from one backup run with `goforge backup restore-service <run key> <instance id>`, run inside the running GoForge container. The current volume contents are kept in `<volume>/.goforge-pre-restore-<UTC time>/`. Admins who own the service see its restore points on the service page with the command. There is no restore in the web UI, the API, or MCP. Other templates (Redis, MinIO, ...) are refused.

## Sub-features

- `svc-restore-plan`: reads only `<run>/manifest.json`; finds `services/<id>/gitea.sqlite3|vaultwarden.sqlite3` and `data.tar.gz`, or for Postgres `roles.sql` (kind `pg_roles`) and every `<database>.dump` (kind `pg_dump`, unique non-empty `database`); refuses a bad run key, a bad instance ID (`../x`), an instance not in the run (says when the run skipped it), a Postgres run without `roles.sql` or dumps, and any other template.
- `svc-restore-dry-run`: without `--yes` prints the run, instance, template, container state, each artifact with size and SHA-256 (Postgres: roles, one `database:` line per dump, and the `superuser:` from `POSTGRES_USER`), the kept-folder path, and the preflight (container exists, backup lock free). Docker sees only an inspect.
- `svc-restore-confirm`: `--yes` asks for the run key on a TTY; with no TTY it needs `--confirm=<run key>`; a wrong key stops before anything is stopped. `--confirm` without `--yes` is refused.
- `svc-restore-integrity`: a size or SHA-256 mismatch (`ErrRestoreIntegrity`) stops before the container is stopped; temp files are removed.
- `svc-restore-lock`: the backup advisory lock is held for the whole restore; a running backup refuses it (`ErrRestoreBackupRunning`).
- `svc-restore-aside`: everything in the volume (except older kept folders) moves to `<volume>/.goforge-pre-restore-<stamp>/`; nothing is deleted.
- `svc-restore-owners`: the tar is extracted as root with its owners; the database file gets its directory's owner and the previous file's mode (Gitea: `1000:1000`).
- `svc-restore-dbpath`: Gitea's database goes to `GITEA__database__PATH`, else `PATH` in the restored `app.ini`, else `/data/gitea/gitea.db`; a path outside `/data` or with `..` is refused.
- `svc-restore-rollback`: a failed extract or Postgres load moves the partial restore to `<volume>/.goforge-failed-restore-<stamp>/`, moves the previous contents back, and starts the service.
- `svc-restore-pg-cluster`: Postgres gets a fresh `initdb` cluster in `<volume>/.goforge-restore-new-<stamp>` as the data directory's owner (root falls back to the image's `postgres`), superuser `POSTGRES_USER` (default `postgres`; over 63 chars, leading `-`, or a newline is refused), the four config files copied from the kept cluster, then moved into place; no build folder is left.
- `svc-restore-pg-load`: a temporary server with `listen_addresses=''` and a private trust-only socket loads `roles.sql` (minus the superuser's own `CREATE ROLE`) with `ON_ERROR_STOP`, then `DROP DATABASE IF EXISTS` + `pg_restore --create --exit-on-error` per dump; role passwords (SCRAM hashes) keep working. Errors are trimmed to 20 lines with SCRAM/md5/`PASSWORD '...'` masked. `POSTGRES_PASSWORD` is never read.
- `svc-restore-helper`: the helper runs the service's own image, `--volumes-from` the service, network `none`, user `0:0`, label `goforge.restore-helper`; it is removed with `RemoveVolumes: false`.
- `svc-restore-health`: the service is started and must report healthy (5 minute bound); unhealthy is an error naming the kept folder.
- `svc-restore-backup-excludes`: the next nightly `data.tar.gz` leaves `.goforge-pre-restore-*` and `.goforge-failed-restore-*` out.
- `svc-restore-points-card`: `GET /services/{id}/restore-points` (admin and owner; `401`/`403` otherwise, `404` when not wired, `Cache-Control: no-store`) lists only runs whose manifest has artifacts with this `instance_id`, newest first, with run key, time in the schedule's zone, size of this instance's artifacts, and the dry-run and `--yes` commands. The service page loads it with `hx-get` on `load`, outside the pending poll region; non-admins never get the card.

## How to get to it (user POV)

```bash
docker compose exec goforge ./goforge backup list
docker compose exec goforge ./goforge backup restore-service <run key> <instance id>          # dry run
docker compose exec goforge ./goforge backup restore-service <run key> <instance id> --yes    # type the run key
```

Or open the service page as the admin owner and read Restore points.

## Driving it

Unit and handler tests (no Docker):

```bash
go tool templ generate
go test ./internal/backup/ -run 'ServiceRestore|RestoreService|ListInstance'   # plan, order, integrity, lock, rollback, scripts run for real on a temp dir
go test ./internal/docker/ -run 'Helper|CopyTo'
go test ./cmd/goforge/ -run 'RestoreService|ServiceRestoreArgs'               # dry run, confirm, --yes
go test ./internal/web/handlers/ -run 'ServiceRestorePoints|ServiceShow_RestorePointsCard'
```

Integration round trips (`svc-restore-*` except the card).

Gitea and Vaultwarden: It starts its own MinIO (`cgr.dev/chainguard/minio`), `gitea/gitea:1.25.5`, and `vaultwarden/server` (names `gfsr-it-*`, throwaway volumes) and removes them. Needs Docker and `sqlite3` on `PATH`. On minis `ret` is not in the `docker` group, so build as `ret` and run the binary with `sudo -n`:

```bash
go test -c -tags integration -o /tmp/gfsr-backup.test ./internal/backup
cd internal/backup && sudo -n env PATH=$PATH /tmp/gfsr-backup.test -test.run 'TestIntegration_RestoreService$' -test.v
```

It backs up both, adds a Gitea user and changes a Vaultwarden table row and marker files, restores each, and checks: the old user/row/markers are back, the new ones are only in the kept folder, Gitea's files are `1000:1000` and Gitea can create a user, both are healthy, no helper is left, and a second backup's tars leave the kept folders out. About 25-50 s on minis.

Postgres (`TestIntegration_RestoreServicePostgres`): its own MinIO and a `postgres:17` service on its own network (names `gfsr-it-*`). No `sqlite3` needed; the checks run `psql` in client containers from the same image.

```bash
cd internal/backup && sudo -n env PATH=$PATH /tmp/gfsr-backup.test -test.run 'TestIntegration_RestoreServicePostgres$' -test.v
```

It seeds two databases and an `app_rw` role, backs up, then adds rows, a database, and a new `app_rw` password, and restores. It checks: dry run leaves the container running; rows, databases, owners, and both passwords (over TCP) match the backup; PGDATA is `postgres` with mode 700 and no `.goforge-restore-new-*`; the kept cluster, started on a copy, holds the changed rows; the service takes writes. Then a run with a garbage `app.dump` (SHA-256 fixed up in the manifest) must fail with "previous data was put back" and leave the service healthy on the pre-restore data. No helper is left; no password appears in the output. About 21 s on minis. To try another image, change `srPgImage` locally (passed once with `postgres:16-alpine`, owner 70:70).

## Gotchas

- Never run it against the live stack or the live bucket. The test only touches `gfsr-it-*` containers.
- Vaultwarden's image has no `sqlite3`; the test changes its database with the Gitea image's `sqlite3` in a one-off `--volumes-from` container while Vaultwarden is stopped.
- The kept folders stay inside the volume and use its disk; delete them by hand. For Postgres the volume needs room for two clusters.
- Postgres: `pg_dumpall` 17 writes `CREATE ROLE` for the superuser, which `initdb` already made; the script drops that one line. Settings pointing at files inside PGDATA (`ssl` certs) and `POSTGRES_INITDB_ARGS` are not carried over; such a restore fails and rolls back.
- `docker compose run --rm goforge` (as for `backup restore`) also works, but `exec` into the running container is enough: restore-service does not touch the GoForge database.
