# Audit log retention

An admin sets how long audit rows are kept. The default is forever. A daily job hard-deletes older rows and writes one `audit.retention.purge` row. Replay protection for an `approval_ref` stays inside the 30-day floor.

## Sub-features

- `retention-off` with no saved row and no `GOFORGE_AUDIT_RETENTION_DAYS`, rows of any age stay. The activity page says rows are kept until deleted by hand.
- `retention-30` saving `30` deletes only rows older than 30 days, leaves a younger row, and writes one `audit.retention.purge` row with `deleted`, `cutoff`, `through_seq`, and `through_hash`.
- `retention-invalid` `10` and `abc` are a field error on `retention_days`. Nothing is saved.
- `retention-admin-only` a non-admin does not see the card on Settings → Profile and `PUT /settings/audit-retention` is `403`.
- `retention-replay` `FindApproval` still returns a row that is inside the window, so the same `approval_ref` is still refused.

## How to get to it (user POV)

- Settings → Profile → **Audit log**, admins only.
- The field is **Retention days**. **Save audit retention** posts `PUT /settings/audit-retention`.
- Settings → Activity shows one line, `data-testid="audit-retention-note"`.

## Driving it

Preconditions: a throwaway database, not the live stack. Host mode on `127.0.0.1:18080` is fine. Read `goforge_csrf` after a GET. Do not print the cookie.

- **Off.** With retention unset, insert is unnecessary: `verify.sh psql` is not available in host mode. Use `psql` on the throwaway database. A row with `created_at` 40 days ago is still there after `PurgeOnce` is not required when the setting is 0; the activity note contains `kept until deleted by hand`.
- **30 days.** As the admin, `PUT /settings/audit-retention` with `retention_days=30` and the CSRF token is `200` and the body contains `data-testid="audit-retention-card"`. `psql` shows `retention_days` 30 and `retention_saved` true. A row dated 40 days ago is gone after the purge; a row dated 10 days ago remains. `select details->>'deleted', details->>'through_seq' from audit_logs where action = 'audit.retention.purge' order by created_at desc limit 1` returns a deleted count and a seq.
- **Invalid.** `retention_days=10` and `retention_days=abc` are `400` and the body contains `retention_days`. The saved value does not change.
- **Non-admin.** The Profile HTML has no `data-testid="audit-retention-card"`. `PUT /settings/audit-retention` is `403`.
- **Replay.** `FindApproval` for the 10-day row still returns it. The API still answers a second call with the same `approval_ref` as a replay. See [api-v1.md](./api-v1.md).

## Gotchas

- `0` and an unset variable both mean forever. The environment variable is ignored once `retention_saved` is true.
- The purge will not delete a row newer than 30 days even if you set the GUC yourself.
- Proof belongs in `/tmp/goforge-verify/proof/audit-retention-verdict.txt`, not in the repository.
