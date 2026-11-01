# Backup & Restore

GoForge can upload a backup every night to S3-compatible storage: Cloudflare R2, Backblaze B2, MinIO, or AWS S3. `goforge backup restore` puts the GoForge database back from a run, and `goforge backup restore-service` puts one Postgres, Gitea, or Vaultwarden service back.

## Turn it on

An admin sets backups up in the web UI: **Settings → Profile → Backups**. The form has every setting below plus an **Enabled** switch. **Save** applies the change right away, with no restart. **Test connection** checks the endpoint, bucket, and key by listing one object under the prefix, and nothing is written. It gives up after 10 seconds. Only admins see the form; other users get `403` from its routes.

- The secret access key is never shown again after it is saved. Leave the field blank to keep the stored key, or type a new one to replace it. The access key ID shows at most its last 4 characters.
- Both keys are stored in the GoForge database, encrypted with `ENCRYPTION_KEY`, like Git source tokens.
- Turning **Enabled** off pauses backups and keeps every other setting, so turning it back on needs no re-entry.
- A run that is already going when you save finishes with the settings it started with. The next run uses the new ones.
- Each save is written to the audit log as `backup.settings.update`, with the names of the changed fields only, never their values.

### From the environment (host runs or an override)

With Docker Compose, use the form above. The repo `docker-compose.yml` does not pass any `BACKUP_*` variable into the `goforge` container, so `BACKUP_*` lines in your env file (`.env`) do nothing there, and the container's environment has no backup keys for `docker inspect` to show.

GoForge still reads `BACKUP_*` from its process environment. That matters in two cases: you run the `goforge` binary on the host (export the variables, or put them in the unit or script that starts it), or you want a one-time import on a compose install. For the import, add the variables to your own `docker-compose.override.yml` next to `docker-compose.yml`:

```yaml
services:
  goforge:
    environment:
      - BACKUP_S3_ENDPOINT=https://<account id>.r2.cloudflarestorage.com
      - BACKUP_S3_BUCKET=goforge-backups
      - BACKUP_S3_ACCESS_KEY_ID=...
      - BACKUP_S3_SECRET_ACCESS_KEY=...
```

Start GoForge (`docker compose up -d goforge`), check the log, then delete those lines from the override and recreate the container (`docker compose up -d goforge`) so the keys leave its environment. How GoForge treats the variables:

1. With no saved settings and `BACKUP_S3_BUCKET` set, GoForge copies the `BACKUP_*` values into the saved settings on its first start and logs `imported BACKUP_* into backup settings`. From then on, edit them in Settings → Profile.
2. Once saved settings exist, they win. Changing `BACKUP_*` afterwards has no effect, and GoForge logs a warning that names the fields that differ. After the import, remove **every** `BACKUP_*` variable, not only the two keys.
3. With no saved settings and no bucket, backups are off.

All the variables, with their defaults (the same defaults apply when a variable is unset):

```bash
BACKUP_S3_ENDPOINT=https://<account id>.r2.cloudflarestorage.com
BACKUP_S3_BUCKET=goforge-backups
BACKUP_S3_ACCESS_KEY_ID=...
BACKUP_S3_SECRET_ACCESS_KEY=...
# Optional
BACKUP_S3_REGION=auto
BACKUP_S3_PREFIX=goforge/
BACKUP_S3_FORCE_PATH_STYLE=false   # true for MinIO
BACKUP_SCHEDULE=02:00 America/Chicago   # or "off"
BACKUP_RETENTION_DAILY=7
BACKUP_TIMEOUT=2h
```

While no saved settings exist, a bucket in the environment with a missing endpoint or key, or another bad `BACKUP_*` value, stops GoForge at startup with a message naming the variable. See [Environment Variables](../configuration/index.md#backups). Once saved settings exist, the same mistake no longer stops GoForge: `BACKUP_*` is ignored and GoForge logs `BACKUP_* in the environment is invalid and ignored because saved backup settings exist`. So a half-removed set (keys gone, bucket still there) still starts. An older GoForge would refuse it, which is one more reason to remove every `BACKUP_*` variable. The form checks the same rules and names the field that is wrong.

### If ENCRYPTION_KEY changes

The saved access key ID and secret are sealed with `ENCRYPTION_KEY`. If GoForge starts with a different key (lost, rotated, or a restore onto a new server), the keys no longer decrypt:

- Backups stay off. GoForge does not fall back to `BACKUP_*`. It logs `saved backup keys do not decrypt with the current ENCRYPTION_KEY; backups stay off until an admin enters both keys again in Settings → Profile → Backups`. `goforge backup run` fails with the same advice.
- Settings → Profile → Backups still shows the endpoint, bucket, prefix, region, schedule, retention, and timeout. Both key fields say the saved key does not decrypt.
- Enter **both** the access key ID and the secret, tick Enabled, and save. GoForge stores them sealed with the current key and backups resume with no restart. A save with only one key, or none, is refused with a message naming the problem. Test connection also needs both keys.

`goforge backup run` uses the same settings as the server: the saved settings if they exist, otherwise `BACKUP_*`.

Use a key that can only read, write, list, and delete in this one bucket. Encryption is the provider's at-rest encryption; GoForge does not encrypt the files itself. Anyone with the key can read the dumps.

!!! danger "Keep ENCRYPTION_KEY outside the server"
    The GoForge database holds service passwords, tokens, and env vars encrypted with `ENCRYPTION_KEY`. The key is never written to a backup, a log line, or a run's error. Without a copy of it, a restored database cannot decrypt those values. Store it, with the rest of your env file (`.env`), in a password manager. If the key is lost, the saved backup keys cannot be decrypted either: backups stay off until an admin enters both keys again in Settings → Profile → Backups (see [If ENCRYPTION_KEY changes](#if-encryption_key-changes)).

## What a run backs up

| Target | Objects | How |
|--------|---------|-----|
| GoForge database | `app/goforge-db.dump` | `pg_dump --format=custom` from the app container, using `DATABASE_URL`. The runtime image ships the PostgreSQL 17 client, which reads servers up to 17 |
| Each running `postgres` service | `services/<id>/<database>.dump`, `services/<id>/roles.sql` | `docker exec` into the service container as its `POSTGRES_USER`: one `pg_dump --format=custom` per non-template database, and `pg_dumpall --roles-only`. GoForge does not decrypt the service's admin password |
| Each running `gitea` service | `services/<id>/gitea.sqlite3`, `services/<id>/data.tar.gz` | SQLite online backup of the database, then a tar of `/data` |
| Each running `vaultwarden` service | `services/<id>/vaultwarden.sqlite3`, `services/<id>/data.tar.gz` | `/vaultwarden backup`, then a tar of `/data` |

Not backed up: Redis and every other template, stopped services (listed as skipped in the manifest), deployed projects' containers and images (rebuilt from Git), Traefik certificates (reissued), and your env file (`.env`).

`roles.sql` holds the SCRAM password hashes of every role in that Postgres service. Treat it like a password file.

### Consistency

Nothing is stopped and no volume is changed, apart from the short-lived files below.

- **Postgres** dumps are each a consistent snapshot of one database (`pg_dump` runs in one transaction). Databases in one service are dumped one after another, not at one instant.
- **Gitea**: `sqlite3 .backup` as the `git` user copies a consistent database while Gitea keeps running. The copy goes to a `mktemp` file in the container's `/tmp`, is streamed out, and is removed. The tar of `/data` leaves out `gitea/gitea.db*`, `gitea/indexers`, and `gitea/queues`. Repositories are tarred live. A push during the tar can leave that repository's newest objects half-copied; older history is unaffected. *Hunch, not measured:* at 02:00 on a small instance this is rare. Run `git fsck` on restored repositories.
- **Vaultwarden**: `/vaultwarden backup` writes `/data/db_<timestamp>.sqlite3` with SQLite's online backup. GoForge checks the path it prints, streams that one file, and deletes only it. The tar of `/data` leaves out `db.sqlite3*`, `db_*.sqlite3`, `icon_cache`, and `tmp`, and keeps `rsa_key*`, `attachments`, `sends`, and `config.json`. A file that changes while tar reads it is accepted.

## Layout, manifest, and retention

```
<prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/app/goforge-db.dump
<prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/services/<id>/...
<prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/manifest.json
```

The date is the run's start date in the schedule's zone (UTC when the schedule is `off`). The second segment is the start time in UTC and is the run id.

`manifest.json` is written last and only when every step succeeded. It lists each object's key, kind, size, and SHA-256, the instances that were skipped and why, the GoForge version, and the start and finish times. A run directory without a manifest is incomplete; do not restore from it.

Retention runs after a successful run only:

- A day counts when it has at least one run with a manifest.
- The newest `BACKUP_RETENTION_DAILY` complete days are kept. Older days are deleted, but only once that many complete days exist, so a run of failures never empties the bucket.
- Inside a kept day, runs older than that day's newest complete run are deleted.
- Keys under the prefix that do not have the `<date>/<run id>/` shape are never touched.

If pruning fails, the run stays `succeeded` and its error says `retention: ...`.

## Schedule and status

The scheduler runs inside `goforge serve`. Missed runs are not caught up: if GoForge is down at 02:00, the next run is the following night. A run interrupted by a restart is marked `failed` when GoForge starts again. A Postgres advisory lock stops two runs from overlapping, including a manual run during a scheduled one.

Every run is a row in `backup_runs` with its status, object count, bytes, skipped count, pruned count, and a one-line error with secrets removed. Failures are also logged. There is no other alert.

- **Settings → Profile** shows admins the last run, the error of a failed run, and the next run.
- **`GET /api/v1/backups`** (read scope, admin account) returns the same status as JSON with the recent runs. See [API v1](../reference/api-v1.md).
- **MCP**: `get_backup_status` calls that endpoint.

There is no run-now endpoint in v1. To run one now:

```bash
docker compose exec goforge ./goforge backup run
```

It prints the result and exits non-zero when the run failed. It does not run migrations; start `goforge serve` once after an upgrade first.

## Restore

### List restore points

```bash
docker compose exec goforge ./goforge backup list
```

It lists the runs under the prefix, newest first: run key, trigger, GoForge version, start time, bytes, and skipped count. Runs without a `manifest.json` are counted as incomplete and cannot be restored. Admins see the same list, read-only, under **Settings → Profile → Backups → Restore points**, with the command to run, and each Postgres, Gitea, or Vaultwarden service page lists the runs that hold that service's data (run key, time, size) with its `restore-service` command. There is no restore button in the web UI, the API, or MCP.

### GoForge database

`goforge backup restore <run key>` restores `app/goforge-db.dump` and nothing else. It never reads a run's `services/...` objects and never touches a service container.

```bash
docker compose stop goforge
# Dry run: prints the plan and the preflight checks, changes nothing
docker compose run --rm --no-deps goforge ./goforge backup restore 2026-10-29/20261029T070000Z
# Restore: type the run key when asked (or pass --confirm=<run key> with no TTY)
docker compose run --rm --no-deps goforge ./goforge backup restore 2026-10-29/20261029T070000Z --yes
docker compose start goforge
```

It uses the same bucket settings as `goforge backup run` (the saved settings, else `BACKUP_*`), and the same `DATABASE_URL`. The steps:

1. Read `manifest.json`. It refuses a run with no manifest, a manifest newer than this binary understands, or one with no `app/goforge-db.dump`.
2. Take the backup advisory lock (refuses while a backup runs) and check that nothing else is connected to the GoForge database. If something is, it refuses with `stop GoForge first: docker compose stop goforge`.
3. Download the dump to a private temp file and check its size and SHA-256 against the manifest. A mismatch refuses.
4. Create a side database `<db>_restore_<run id>` and load the dump into it: `pg_restore -f -` (PostgreSQL 17 client) piped into `psql` with `ON_ERROR_STOP` in one transaction. The `SET transaction_timeout` line that pg_dump 17 writes is dropped so the load also works on the PostgreSQL 15 server the compose file ships. A broken dump fails here and the live database is untouched.
5. Check the side database: its `schema_migrations` must not be newer or dirty for this binary (a newer schema refuses: upgrade GoForge first). Delete every session (everyone signs in again; API tokens stay as they were in the backup) and write an audit row `backup.restore` with the run key only.
6. In one transaction, rename the current database to `<db>_pre_restore_<UTC time>` and the side database to `<db>`. The previous database is **kept** and never dropped automatically.

On the next start, `goforge serve` runs any newer migrations. The restored database keeps the backup settings row from the dump; if its bucket or prefix differ from the ones you restored from, the restore prints a warning.

No password, key, or `DATABASE_URL` goes on a command line or into the output: the clients get the connection from the environment.

After a restore:

- `ENCRYPTION_KEY` must be the one in use when the backup was taken, or saved env vars, service secrets, and backup keys do not decrypt.
- Service records match the backup. Services created later are not listed; removed ones show as missing. Their containers and data were not touched.
- To undo, with GoForge stopped, rename the databases back. The command prints the exact SQL, for example:

```sql
ALTER DATABASE goforge RENAME TO goforge_undone;
ALTER DATABASE goforge_pre_restore_20261030t172858z RENAME TO goforge;
```

- When you no longer need it: `DROP DATABASE goforge_pre_restore_...;`

!!! warning "Do not use `pg_restore` from the db container"
    Older versions of this page said `docker compose exec -T db pg_restore ...`. That fails: the `db` container is PostgreSQL 15, and its `pg_restore` cannot read a dump made by the PostgreSQL 17 `pg_dump` in the GoForge image (`unsupported version (1.16) in file header`). It also restored over the live database in place. Use `goforge backup restore`.

### Download a run by hand

For service data, download the run with any S3 client and check each object's SHA-256 against `manifest.json` first:

```bash
# With the AWS CLI against R2:
aws s3 cp --recursive --endpoint-url "$BACKUP_S3_ENDPOINT" \
  "s3://$BACKUP_S3_BUCKET/goforge/2026-10-29/20261029T070000Z/services/" ./restore/services/
```

### A service: Postgres, Gitea, or Vaultwarden

`goforge backup restore-service` restores one Postgres, Gitea, or Vaultwarden service from one run. It runs inside the running GoForge container (it needs the Docker socket; GoForge keeps serving). Find the instance ID on the service page (it is in the URL, `/services/<id>`); admins also see the service's restore points there, with the command to copy. Other service types (Redis, MinIO, and the rest) are refused: they have no service backup to restore.

```bash
# Dry run: reads the manifest, inspects the container, checks the backup lock; changes nothing
docker compose exec goforge ./goforge backup restore-service 2026-10-29/20261029T070000Z <instance-id>
# Restore: type the run key when asked (or pass --confirm=<run key> with no TTY)
docker compose exec goforge ./goforge backup restore-service 2026-10-29/20261029T070000Z <instance-id> --yes
```

With `--yes` it:

1. Takes the backup lock, so no backup run can read the service halfway.
2. Downloads the service's files and checks each size and SHA-256 against `manifest.json`: for Postgres, `services/<id>/roles.sql` and every `services/<id>/<database>.dump`; for Gitea or Vaultwarden, `gitea.sqlite3` (or `vaultwarden.sqlite3`) and `data.tar.gz`. A mismatch stops here, with the service still running.
3. Stops the service container.
4. Starts a helper container from the service's own image with `--volumes-from` the service, no network, as root, and copies the files into it.
5. Moves everything in the data volume (Postgres: `/var/lib/postgresql/data`; Gitea and Vaultwarden: `/data`) to `<volume>/.goforge-pre-restore-<UTC time>/` (kept, never deleted), then restores:
    - **Postgres**: `initdb` builds a new, empty cluster as the data directory's owner, with the service's `POSTGRES_USER` as superuser, and copies `postgresql.conf`, `postgresql.auto.conf`, `pg_hba.conf`, and `pg_ident.conf` over from the kept cluster. A temporary server starts on a private socket only (no TCP), loads `roles.sql` (role names, attributes, and password hashes, so existing passwords keep working), then runs `pg_restore --create --exit-on-error` for each dump. The server stops and the new cluster is moved into place. GoForge never reads the service's admin password.
    - **Gitea or Vaultwarden**: extracts `data.tar.gz` with its file owners, and copies the SQLite snapshot to the database path (Gitea: `GITEA__database__PATH`, else `PATH` in the restored `app.ini`, else `/data/gitea/gitea.db`; Vaultwarden: `/data/db.sqlite3`) with the owner of its directory.
6. Removes the helper (never its volumes), starts the service, and waits for its health check.

If step 5 fails (a bad dump, a role that cannot be created, a full disk), the partial restore is moved to `<volume>/.goforge-failed-restore-<UTC time>/`, the previous contents are moved back, and the service is started again on its old data. Nightly backups leave both kinds of kept folder out of `data.tar.gz`; Postgres backups are logical dumps, so a kept cluster is never in them. Delete a kept folder by hand once the restored service checks out:

```bash
docker exec goforge-service-<id> rm -rf /data/.goforge-pre-restore-<UTC time>
# Postgres:
docker exec goforge-service-<id> rm -rf /var/lib/postgresql/data/.goforge-pre-restore-<UTC time>
```

To undo a restore, restore a newer run, or stop the service and move the contents of the kept folder back to the volume.

The service record in GoForge (name, config, domain) is not changed, only the volume. Data written after the backup is gone: for Postgres, rows, databases, and role changes made since (restart apps that hold connections); for Gitea, pushes and issues; for Vaultwarden, items saved since (clients sync to the restored state). For Gitea, run `git fsck` in the restored repositories and, if search looks wrong, rebuild the indexes from the admin panel (they are not backed up).

Postgres limits:

- The volume needs room for two copies of the cluster while the kept folder is there.
- Only the four config files above are carried over. A setting that points at a file inside the data directory (for example `ssl = on` with certificates there) fails the restore, which rolls back; copy the files in and restore again, or restore by hand.
- `initdb` runs with the image defaults; options from `POSTGRES_INITDB_ARGS` (data checksums, locale) are not applied again. Databases keep the encoding and locale recorded in their dumps.
- Extensions must be in the image (the same image is used, so a dump from that service works).

Drilled:

- `TestIntegration_RestoreServicePostgres` backs up a throwaway `postgres:17` service (two databases, an app role with its own password) into MinIO, changes it (new rows, a new database, a new app password), and restores it with `ServiceRestorer`. It checks the backed-up rows, databases, owners, and both passwords are back over TCP; the data directory is owned by `postgres` with mode 700 and no build folder is left; the kept folder holds the changed cluster (started on a copy); the service reports healthy and takes writes; no helper is left and no secret appears in the output. It then restores a run with a garbage dump whose SHA-256 matches the manifest: the restore fails, the previous data is put back, and the service is healthy on it. First drilled on November 1, 2026, also run once against `postgres:16-alpine`.
- `TestIntegration_RestoreService` backs up throwaway `gitea/gitea:1.25.5` and `vaultwarden/server` containers into MinIO, changes them (a Gitea user, a Vaultwarden table row, marker files), restores each with `ServiceRestorer`, and checks the backed-up state is back, the changed state is in the kept folder, Gitea's files are owned by `git` (1000) and it can write its database, both report healthy, no helper is left, and the next backup leaves the kept folder out. First drilled on November 1, 2026.

## Smoke test with throwaway MinIO

The integration test in `internal/backup` starts MinIO, Postgres, Gitea, and Vaultwarden in throwaway containers, runs a real backup, and checks every object. A second test (`TestIntegration_Restore`) backs up into MinIO and restores into throwaway `postgres:15-alpine` and `postgres:17-alpine` servers, including every refusal. A third (`TestIntegration_RestoreService`) round-trips throwaway Gitea and Vaultwarden through `restore-service`, and a fourth (`TestIntegration_RestoreServicePostgres`) does the same for a throwaway Postgres service, including a rollback. See `.cursor/skills/verify-goforge/features/backups.md` and `service-restore.md`. `docker.io/minio/minio` did not pull on our host in October 2026, so the test uses `cgr.dev/chainguard/minio`.

To try `goforge backup run` against a local MinIO:

```bash
docker run -d --name backup-minio -p 127.0.0.1:9000:9000 \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=change-me-please \
  cgr.dev/chainguard/minio:latest server /data
# Create the bucket with any S3 client, then:
BACKUP_S3_ENDPOINT=http://127.0.0.1:9000 BACKUP_S3_BUCKET=goforge-backups \
BACKUP_S3_ACCESS_KEY_ID=minioadmin BACKUP_S3_SECRET_ACCESS_KEY=change-me-please \
BACKUP_S3_FORCE_PATH_STYLE=true ./bin/goforge backup run
```

Point `DATABASE_URL` at a test database. On a host with live services, this also dumps every running Postgres, Gitea, and Vaultwarden service GoForge knows about.
