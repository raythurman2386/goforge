# Allows backups switch

A service owner turns **Allows backups** on or off on the page of a service whose template declares a usable `backup.strategy` (shipped: Postgres, Gitea, Vaultwarden, MinIO). The nightly run and `goforge backup run` include the instance only when the switch is on and the container is running. Each change writes a `service.set_backup` audit row. Templates without a strategy (Redis and the rest) have no switch and the route refuses them.

## Sub-features

- `backup-csrf`: `POST /services/$ID/backup` with no CSRF token, or a wrong one, is `403`; `service_instances.backup_enabled` is unchanged. With no session it redirects to login.
- `backup-non-owner`: a second account posting to another user's service is `403`, with no change and no audit row.
- `backup-toggle-off`: `backup_enabled=false` answers `200`, the card reads `Left out of nightly backups`, and `select backup_enabled from service_instances where id = '$ID'` is `f`.
- `backup-toggle-on`: `backup_enabled=true&backup_enabled=false` (the switch plus its hidden field) answers `200`, the card reads `Included in nightly backups` and is checked, and the column is `t`.
- `backup-audit`: each real change writes one `service.set_backup` row with `details.via` `ui`, `details.previous`, and `details.backup_enabled`. Posting the same value again answers `200` and writes no row.
- `backup-minio`: an existing MinIO instance (strategy `volume`) starts `f`; turning it on stores `t`.
- `backup-unsupported`: for a `redis` instance the post is `409`, the column is unchanged, and the service page has no `data-testid="service-backup-switch"`.
- `backup-api`: `GET /api/v1/services` and `GET /api/v1/services/{id}` with a `read` token return `backup_enabled` for each service.
- `backup-manifest-skip`: with the switch off, the next run's `manifest.json` lists the instance under skipped with reason `backups disabled for this instance` (unit: `TestInstanceSource`).

## How to get to it (user POV)

- Services → a Postgres, Gitea, Vaultwarden, or MinIO service → **Allows backups** card. The switch posts on change; the card text says whether the instance is in nightly backups. Navigating away and back shows the stored state.

## Driving it with curl

- Preconditions: `drive-register`, `drive-login`, and throwaway service rows this run created (a `stopped` row per template is enough for the switch; never touch a service this run did not start). The CSRF header comes from the jar (`goforge_csrf`) as in the other UI drives.
- Toggle: `curl -b $JAR -H "X-CSRF-Token: $CSRF" --data 'backup_enabled=false' $BASE/services/$ID/backup`, then `--data 'backup_enabled=true&backup_enabled=false'`.
- Switch present: `curl -b $JAR $BASE/services/$ID | grep -c 'data-testid="service-backup-switch"'` is `1` for postgres, gitea, vaultwarden, minio and `0` for redis.
- Audit: `verify.sh psql "select details->>'backup_enabled', details->>'previous', details->>'via' from audit_logs where resource_id = '$ID' and action = 'service.set_backup' order by created_at"`.
- API: create a `read` token on Profile, then `curl -H "Authorization: Bearer $TOK" $BASE/api/v1/services` and check `backup_enabled`.
- No `verify.sh` subcommand drives this yet. Gatekeeper drove the steps above in host mode for PR #137.

## Gotchas

- The switch posts `backup_enabled=true` plus a hidden `backup_enabled=false`; any `true` means on.
- Migration 032 sets existing Postgres, Gitea, and Vaultwarden rows on and existing MinIO rows off; a new deploy of any of the four starts on.
- The change applies on the next run with no restart. Template files are reloaded at the start of a run; one broken template is logged and does not stop the run.
- Tests: `TestServiceHandler_SetBackupEnabled_TogglesAndAudits`, `TestServiceHandler_SetBackupEnabled_NonOwnerRefused`, `TestServiceHandler_SetBackupEnabled_UnsupportedTemplate` (handlers), `TestServiceInstanceRepository_BackupEnabled`, `TestBackupCardStates`, `TestDetailBackupCardOnlyWhenOffered`, `TestInstanceSource`, `TestInstanceSource_BrokenTemplateReloadDoesNotFailTargets`, `TestMigration032_DownAndUp`.
