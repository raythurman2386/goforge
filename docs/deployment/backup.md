# Backup & Restore

GoForge can upload a backup every night to S3-compatible storage: Cloudflare R2, Backblaze B2, MinIO, or AWS S3. It is backup only. Restoring is a manual runbook, below.

## Turn it on

Set these in `.env` and restart GoForge (`docker compose up -d goforge`):

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

Backups are off while `BACKUP_S3_BUCKET` is empty. With a bucket set, a missing endpoint or key stops GoForge at startup with a message naming the variable. See [Environment Variables](../configuration/index.md#backups).

Use a key that can only read, write, list, and delete in this one bucket. Encryption is the provider's at-rest encryption; GoForge does not encrypt the files itself. Anyone with the key can read the dumps.

!!! danger "Keep ENCRYPTION_KEY outside the server"
    The GoForge database holds service passwords, tokens, and env vars encrypted with `ENCRYPTION_KEY`. The key is never written to a backup, a log line, or a run's error. Without a copy of it, a restored database cannot decrypt those values. Store it, with the rest of `.env`, in a password manager.

## What a run backs up

| Target | Objects | How |
|--------|---------|-----|
| GoForge database | `app/goforge-db.dump` | `pg_dump --format=custom` from the app container, using `DATABASE_URL`. The runtime image ships the PostgreSQL 17 client, which reads servers up to 17 |
| Each running `postgres` service | `services/<id>/<database>.dump`, `services/<id>/roles.sql` | `docker exec` into the service container as its `POSTGRES_USER`: one `pg_dump --format=custom` per non-template database, and `pg_dumpall --roles-only`. GoForge does not decrypt the service's admin password |
| Each running `gitea` service | `services/<id>/gitea.sqlite3`, `services/<id>/data.tar.gz` | SQLite online backup of the database, then a tar of `/data` |
| Each running `vaultwarden` service | `services/<id>/vaultwarden.sqlite3`, `services/<id>/data.tar.gz` | `/vaultwarden backup`, then a tar of `/data` |

Not backed up: Redis and every other template, stopped services (listed as skipped in the manifest), deployed projects' containers and images (rebuilt from Git), Traefik certificates (reissued), and `.env`.

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

The date is the run's start date in the `BACKUP_SCHEDULE` zone (UTC when the schedule is `off`). The second segment is the start time in UTC and is the run id.

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

GoForge has no restore command. These are manual steps. Check each object's SHA-256 against `manifest.json` first.

### Download a run

```bash
# Any S3 client works. With the AWS CLI against R2:
aws s3 cp --recursive --endpoint-url "$BACKUP_S3_ENDPOINT" \
  "s3://$BACKUP_S3_BUCKET/goforge/2026-10-29/20261029T070000Z/" ./restore/
```

### GoForge database

```bash
docker compose stop goforge
docker compose exec -T db pg_restore -U goforge -d goforge --clean --if-exists < restore/app/goforge-db.dump
# Put the original ENCRYPTION_KEY in .env before starting.
docker compose start goforge
```

### A Postgres service

Load `roles.sql` first if the roles are missing, then restore each database:

```bash
ctr=goforge-service-<id>
docker exec -i "$ctr" sh -c 'psql -U "$POSTGRES_USER" -d postgres' < restore/services/<id>/roles.sql
docker exec -i "$ctr" sh -c 'pg_restore -U "$POSTGRES_USER" -d "$1" --clean --if-exists --create' sh postgres \
  < restore/services/<id>/<database>.dump
```

`roles.sql` reports errors for roles that already exist, including `POSTGRES_USER`; that is expected.

### Gitea or Vaultwarden

Stop the service from GoForge first. Then, into a fresh volume or after moving the old files aside (never into a running container):

```bash
ctr=goforge-service-<id>
# Gitea: extract into /data, then put the database back
docker run --rm --volumes-from "$ctr" -v "$PWD/restore/services/<id>:/r" alpine \
  sh -c 'tar -xzf /r/data.tar.gz -C /data && cp /r/gitea.sqlite3 /data/gitea/gitea.db && chown -R 1000:1000 /data'
# Vaultwarden: the database file is /data/db.sqlite3
docker run --rm --volumes-from "$ctr" -v "$PWD/restore/services/<id>:/r" alpine \
  sh -c 'tar -xzf /r/data.tar.gz -C /data && cp /r/vaultwarden.sqlite3 /data/db.sqlite3'
```

Start the service and check it. For Gitea, run `git fsck` in the restored repositories and, if search looks wrong, rebuild the indexes from the admin panel (they are not backed up).

These restore steps were written from the formats, not drilled against a live instance yet.

## Smoke test with throwaway MinIO

The integration test in `internal/backup` starts MinIO, Postgres, Gitea, and Vaultwarden in throwaway containers, runs a real backup, and checks every object. See `.cursor/skills/verify-goforge/features/backups.md`. `docker.io/minio/minio` did not pull on our host in October 2026, so the test uses `cgr.dev/chainguard/minio`.

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
