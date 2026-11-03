# Audit log integrity

Audit rows and their seals reject ordinary `UPDATE`, `DELETE`, and `TRUNCATE`. A keyed hash chain detects a change made by someone who can write the database but cannot read `ENCRYPTION_KEY`.

## Sub-features

- `integrity-trigger` without `goforge.audit_purge=on`, `UPDATE`, `DELETE`, and `TRUNCATE` fail with the append-only error. Any session can set that flag. The table owner can `DISABLE TRIGGER`. The triggers stop accidents. The chain does the detection.
- `integrity-verify-ok` `goforge audit verify` exits 0 and prints `audit chain verified head_seq=N head_hash=...`.
- `integrity-tamper` after `SET session_replication_role = replica` and an edit of a sealed row, verify exits 1 with `hash mismatch` and that seq. Deleting the row and leaving the seal exits 1 with `missing row`.
- `integrity-boundary` after a retention purge, verify still exits 0. The oldest remaining seal's `prev_hash` matches the newest purge row's `through_hash`.
- `integrity-activity` an admin sees `data-testid="audit-integrity"` on Settings → Activity. A non-admin does not.
- `integrity-anchor` a backup `manifest.json` includes `audit_chain_head`. `goforge serve` logs `audit chain verified`.

## How to get to it

- `goforge audit verify` from the host, with the same `DATABASE_URL` and `ENCRYPTION_KEY` as the server.
- Optional `--from-seq N` starts at that seal.
- Settings → Activity, as an admin, shows the last check.

## Driving it

Use a throwaway Postgres. Do not run these statements on a live GoForge database. The app role cannot set `session_replication_role`; use the database superuser and say so in the proof.

```bash
psql -c "update audit_logs set action='x' where id=(select id from audit_logs limit 1)"   # ERROR, append-only
psql -c "delete from audit_logs"                                                          # ERROR, append-only
psql -c "truncate audit_logs"                                                             # ERROR, append-only
./goforge audit verify; echo $?                                                           # 0
psql -c "set session_replication_role=replica; update audit_logs set action='x' where id='<sealed id>'"
./goforge audit verify; echo $?                                                           # 1, names the seq, reason hash mismatch
```

Put the action back the same way before the missing-row check, or use a copy of the database. Then delete that one row under `session_replication_role=replica` and expect `missing row`.

## Gotchas

- The chain proves rows are unchanged since they were sealed. On an upgrade, the first sealer pass seals existing rows. It does not prove they were unchanged before that.
- `goforge.audit_purge` is not a privilege. Any database session can set it, and the table owner can disable the triggers. A boundary whose matching `audit.retention.purge` row has no valid chain-key `purge_mac` makes `goforge audit verify` exit 1 (`boundary without purge`), including after the sealer seals a forged row.
- A delete in the few seconds before a row is sealed is not detected.
- Host root and the Docker socket can read `ENCRYPTION_KEY` and rewrite the chain, the log line, and the backup. This feature does not claim to stop that.
- Restoring a backup taken with a different `ENCRYPTION_KEY` fails verify. That is expected.
- There is no insert-only database role in this release.
- Proof belongs in `/tmp/goforge-verify/proof/audit-integrity-verdict.txt`, not in the repository.
