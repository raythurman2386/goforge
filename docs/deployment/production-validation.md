# Production Validation Plan

!!! warning "Plan only. None of these drills has been run yet."
    This page is a plan. As of v0.3.0 (2026-11-02) no load test, resource limit check, or restore drill has been run against a full GoForge stack and recorded. Do not read this page as evidence that GoForge is production ready. The README note "Not Production-Ready for Untrusted Environments" stays until the records below exist.

GoForge is used every day on one host (Gitea and about 14 other containers), and that has caught real bugs. Daily use is not a validation record: nobody wrote down the load, the limits, or a restore against real backup data. This plan says what to run, where, and what to write down. Running it is scheduled for the v0.8.0 to v0.9.x cycles in the [Roadmap](https://github.com/raythurman2386/goforge/blob/main/ROADMAP.md#release-plan), and a committed record is one of the blockers on the Path to v1.0.

## What is already covered by tests

These run in throwaway containers from the test suite. They are useful, but they are not drills on a full stack:

- `TestIntegration_Restore` restores the GoForge database into throwaway `postgres:15-alpine` and `postgres:17-alpine` servers, including every refusal.
- `TestIntegration_RestoreService` and `TestIntegration_RestoreServicePostgres` round-trip throwaway Gitea, Vaultwarden, and Postgres services through `restore-service`, including a rollback. See [Backup & Restore](backup.md#a-service-postgres-gitea-or-vaultwarden).
- `formal/deploy-retire/` model-checks redeploy grace and retire of the previous container.
- The `verify-goforge` skill (`.cursor/skills/verify-goforge/`) drives the UI on a throwaway Compose stack, including `backups.md`, `backup-restore.md`, and `service-restore.md`.

None of these puts concurrent load on a deploy queue, measures a container against its limits, or restores a real nightly run into a running stack.

## Rules for every drill

- Use a throwaway stack only. The `verify-goforge` helper starts one as Compose project `goforge-verify` with generated secrets (`.cursor/skills/verify-goforge/scripts/verify.sh launch`). Never point a drill at the live stack (`goforge-goforge-1`) or its database, volumes, or backup bucket.
- Use a throwaway bucket (for example a local MinIO, see [Smoke test with throwaway MinIO](backup.md#smoke-test-with-throwaway-minio)) or a copy of a real run in a separate prefix. A drill must never write to, prune, or lock the production backup prefix.
- Note the host: CPU, memory, disk, Docker version, GoForge version (`goforge --version`), and the commit.
- Keep evidence outside the repo (the verify skill uses `/tmp/goforge-verify/proof`). Commit only the written record, with no secrets, tokens, or connection strings.
- Times in the record are America/Chicago, with the zone written next to them.

## Drill 1: concurrent deploys

Goal: show what happens when several deploys are queued at once, and that nothing is lost or left half done.

Known behavior to confirm: the deployment worker runs one deployment at a time, and `MAX_CONCURRENT_BUILDS` is accepted but unused (see the [configuration reference](../configuration/index.md)). So this drill measures queueing, not parallel builds.

Steps:

1. Create 5 small projects from a test repository with a quick Docker build and a health check.
2. Trigger a deploy on all 5 within a few seconds, from the UI and through `/api/v1` with a scoped token.
3. Then trigger 3 redeploys of one project inside one grace period.
4. Restart the `goforge` container while the queue is not empty.

Record:

- Queue wait and total time for each deploy, from the deployment history.
- Final status of every deployment, and whether any stayed `pending`, `building`, or `deploying` after the restart.
- Containers left running per project after the redeploys (expected: exactly one, the newest).
- `/health/ready` response time while the queue is busy, and whether the UI stayed usable.
- Audit rows: each trigger and its linked `deployment.run.*` outcome row.

Pass: every deploy ends in a final state, the queue survives the restart, exactly one container per project is left, and no outcome row is missing.

## Drill 2: per-container limits

Goal: show that CPU and memory limits hold, and how GoForge reports a container that hits them.

Steps:

1. Deploy a one-click service whose template sets `resources` (for example Redis, 512 MiB). Check the Container card and `docker inspect` show the same CPU and memory limits as the template.
2. Run a load inside it that exceeds the memory limit, and one that pins the CPU.
3. Set CPU and memory limits on a project environment, deploy it, and check `docker inspect` on the new container.

Record:

- Limits on the Container card and in `docker inspect`, for the service and for the project deployment.
- What happens at the memory limit: OOM kill, the OOM flag on the Container card, restart count, and whether the container health watch sends `container.exited` or `container.unhealthy` (with notifications on, to a throwaway ntfy topic).
- CPU usage shown in the stats stream against the CPU limit.
- Whether other containers and the GoForge UI stayed responsive.

Open question for this drill: reading the code at v0.3.0, one-click service limits come from the template `resources`, while the environment CPU and memory fields are stored and returned by `/api/v1` but the deploy pipeline does not appear to pass them to Docker. Step 3 confirms or refutes that. If they are not applied, record it as a finding; do not fix it inside the drill.

Pass: service limits match the template and hold under load, an OOM kill is visible in the UI and alerted, and the host stays responsive. Environment limits either apply or are recorded as an open finding.

## Drill 3: restore on a throwaway stack

Goal: restore real data from a real backup run into a stack that is not the live one, and prove the restored services work.

Steps:

1. On the throwaway stack, deploy Postgres, Gitea, and Vaultwarden services and put known data in each (a table with rows, a repository with commits and an issue, a vault item).
2. Turn on backups to the throwaway bucket and run `goforge backup run`. Note the run key.
3. Change the data in each service (new rows, a new commit, a deleted item).
4. For each service, run `goforge backup restore-service <run key> <instance id>` as a dry run, then with `--yes`.
5. Stop GoForge and run `goforge backup restore <run key>` for the GoForge database, as a dry run, then with `--yes`. Start GoForge again.
6. Optional, later: copy one real nightly run into the throwaway bucket under a separate prefix and repeat steps 4 and 5 against it, so the drill covers production-sized data.

Record:

- Run key, artifact sizes, and how long each restore took, including how long each service was stopped.
- That the known data is back and the changed data is in the kept `.goforge-pre-restore-<UTC time>` folder.
- Service health after the restore: Postgres accepts connections with the old passwords, Gitea passes `git fsck` and can push, Vaultwarden clients sync.
- The `backup.restore` audit row for the database restore, and the sessions cleared by it.
- Anything in the [Backup & Restore](backup.md#restore) runbook that was wrong or missing.

Pass: every restore finishes, the known data is back, the services are healthy, and the kept folders hold the changed data.

## Record template

Commit one record per drill run, for example `docs/deployment/validation-records/<date>-<drill>.md`:

```markdown
# <Drill name>, <YYYY-MM-DD>

- Operator:
- GoForge version and commit:
- Host (CPU, memory, disk, Docker version):
- Stack: throwaway (Compose project name)
- Start and end time (America/Chicago):

## Steps run
## Results (numbers, statuses, screenshots kept outside the repo)
## Findings (each with severity and a follow-up issue or PR)
## Verdict: pass, pass with findings, or fail
```

## Exit criteria for v1.0

- A passing record for each of the three drills on the version that is about to be tagged v1.0.0, or a record with findings that Raymond accepts in writing.
- Findings from the drills are fixed or listed as known limits in `SECURITY.md` or the README.
- Only then is the README "Not Production-Ready" note revisited.
