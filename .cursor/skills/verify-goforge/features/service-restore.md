# Restore a Gitea or Vaultwarden service from a backup run

An operator restores one Gitea or Vaultwarden service's `/data` volume from one backup run with `goforge backup restore-service <run key> <instance id>`, run inside the running GoForge container. The current volume contents are kept in `/data/.goforge-pre-restore-<UTC time>/`. Admins who own the service see its restore points on the service page with the command. There is no restore in the web UI, the API, or MCP, and Postgres services are not handled (manual runbook in `docs/deployment/backup.md`).

## Sub-features

- `svc-restore-plan`: reads only `<run>/manifest.json`; finds `services/<id>/gitea.sqlite3|vaultwarden.sqlite3` and `data.tar.gz`; refuses a bad run key, a bad instance ID (`../x`), an instance not in the run (says when the run skipped it), and a Postgres instance.
- `svc-restore-dry-run`: without `--yes` prints the run, instance, template, container state, both artifacts with size and SHA-256, the kept-folder path, and the preflight (container exists, backup lock free). Docker sees only an inspect.
- `svc-restore-confirm`: `--yes` asks for the run key on a TTY; with no TTY it needs `--confirm=<run key>`; a wrong key stops before anything is stopped. `--confirm` without `--yes` is refused.
- `svc-restore-integrity`: a size or SHA-256 mismatch (`ErrRestoreIntegrity`) stops before the container is stopped; temp files are removed.
- `svc-restore-lock`: the backup advisory lock is held for the whole restore; a running backup refuses it (`ErrRestoreBackupRunning`).
- `svc-restore-aside`: everything in `/data` (except older kept folders) moves to `/data/.goforge-pre-restore-<stamp>/`; nothing is deleted.
- `svc-restore-owners`: the tar is extracted as root with its owners; the database file gets its directory's owner and the previous file's mode (Gitea: `1000:1000`).
- `svc-restore-dbpath`: Gitea's database goes to `GITEA__database__PATH`, else `PATH` in the restored `app.ini`, else `/data/gitea/gitea.db`; a path outside `/data` or with `..` is refused.
- `svc-restore-rollback`: a failed extract moves the partial restore to `/data/.goforge-failed-restore-<stamp>/`, moves the previous contents back, and starts the service.
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

Integration round trip (`svc-restore-*` except the card). It starts its own MinIO (`cgr.dev/chainguard/minio`), `gitea/gitea:1.25.5`, and `vaultwarden/server` (names `gfsr-it-*`, throwaway volumes) and removes them. Needs Docker and `sqlite3` on `PATH`. On minis `ret` is not in the `docker` group, so build as `ret` and run the binary with `sudo -n`:

```bash
go test -c -tags integration -o /tmp/gfsr-backup.test ./internal/backup
cd internal/backup && sudo -n env PATH=$PATH /tmp/gfsr-backup.test -test.run 'TestIntegration_RestoreService$' -test.v
```

It backs up both, adds a Gitea user and changes a Vaultwarden table row and marker files, restores each, and checks: the old user/row/markers are back, the new ones are only in the kept folder, Gitea's files are `1000:1000` and Gitea can create a user, both are healthy, no helper is left, and a second backup's tars leave the kept folders out. About 50 s on minis.

## Gotchas

- Never run it against the live stack or the live bucket. The test only touches `gfsr-it-*` containers.
- Vaultwarden's image has no `sqlite3`; the test changes its database with the Gitea image's `sqlite3` in a one-off `--volumes-from` container while Vaultwarden is stopped.
- The kept folders stay inside the volume and use its disk; delete them by hand.
- `docker compose run --rm goforge` (as for `backup restore`) also works, but `exec` into the running container is enough: restore-service does not touch the GoForge database.
