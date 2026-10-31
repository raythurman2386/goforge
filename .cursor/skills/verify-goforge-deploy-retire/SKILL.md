---
name: verify-goforge-deploy-retire
description: "Model-check the GoForge redeploy lifecycle (blue-green grace, retire of the previous container, RecoverPendingRetires after a goforge restart) with TLA+ and TLC. Use when a change touches internal/deploy/cleanup.go, the end of RunPipeline in internal/deploy/pipeline.go, the RecoverPendingRetires call in cmd/goforge/main.go, Docker StopContainer/RemoveContainer, or deployment/container status handling, or when asked whether old containers can be left running or the newest one retired."
---

# Verify GoForge deploy/retire

## The rule

**Property.** After a successful redeploy, once the blue-green grace period is over (or `RecoverPendingRetires` has finished after a goforge restart), at most one container of that project environment is running, every older deployment's container is removed from the host (not just stopped), and the newest deployment's container is never the one retired.

**Harm.** Breaking it leaves a ghost container serving or eating resources beside the new one forever (#95, #96), or takes the site down by retiring the container that should be serving.

**Tool.** TLA+ checked by TLC. The bugs here are orderings: a restart between the grace timer and the retire, two cleanup goroutines interleaving, a Docker call failing between two DB writes. A model checker tries every order of those steps within the bounds below; Go unit tests try only the orders someone wrote.

## Files

| Path | What |
|---|---|
| `formal/deploy-retire/DeployRetire.tla` | The model. Every action names the Go function it stands for. |
| `formal/deploy-retire/DeployRetire.cfg` | Code as shipped, 1 redeploy, up to 2 restarts. Every rule must hold. |
| `formal/deploy-retire/DeployRetireBackToBack.cfg` | Code as shipped, 2 redeploys, 1 restart. Every rule must hold (F1 fixed). |
| `formal/deploy-retire/DeployRetireDockerErrors.cfg` | Code as shipped, Docker stop/rm may error. Every rule must hold (F2 fixed). |
| `formal/deploy-retire/mutants/Pre95NoRemove.cfg` | Mutant: no `RemoveContainer` (code before #95). Must break `NoGhostAfterRetire`. |
| `formal/deploy-retire/mutants/Pre96NoRecover.cfg` | Mutant: no boot recovery (code before #96). Must break `AfterGraceAtMostOneRunning`, `NoGhostAfterRetire`. |
| `formal/deploy-retire/mutants/PreF1RetireNewer.cfg` | Mutant: `RetireOnlyOlder = FALSE`, retire skips only the current deployment (code before the F1 fix). Must break `NewestKeepsRunning`. |
| `formal/deploy-retire/mutants/PreF2ExitOnError.cfg` | Mutant: `RetryFailedRetire = FALSE`, row marked exited after a failed remove, no retry (code before the F2 fix). Must break `AfterGraceAtMostOneRunning`, `NoGhostAfterRetire`. |
| `formal/deploy-retire/view.ts` | Reader labels for the `/statechart` chart. A cleanup waiting in `retry` is the phase "retire failed, retry pending" (group `retry`), never "settled"; `RetryRetire` reads "cleanup retries the retire". |
| `formal/deploy-retire/check.sh` | The one rerun command (below). |
| `internal/deploy/cleanup_replay_test.go` | Go replays of F1 and F2 against the real code. They failed before the fix; the `formalreplay` tag is dropped so `make test` keeps them fixed. |

Each config's `\* expect-break:` line is the contract `check.sh` enforces.

## Does my change touch this?

Yes if the diff changes any of these (the coverage record):

| Model action / rule | Code (at the sync commit) |
|---|---|
| `DeploymentRunning` | `internal/deploy/pipeline.go` `RunPipeline` tail: `dockerClient.CreateContainer`, `containerRepo.Create` (status running), `performHealthCheck`, `Transition(..., DeploymentStatusRunning)`, `go s.CleanupOldDeployments(cleanupCtx, envID, deployID)` |
| `GraceElapses`, `WaitGraceReturns` | `internal/deploy/cleanup.go` `waitGrace`, `gracePeriod` (`BlueGreenConfig.GracePeriod`, default 5m in `bluegreen.go`), `runningSince` |
| `ListByStatus`, `Keep` | `retireSuperseded`: `deploymentRepo.ListByStatus(ctx, running)` filtered to `environmentID`, every running deployment with no row limit (current via the list, else `GetByID`), one `time.Now()`; `keep` = newest running deployment that is current or newer with `now.Sub(runningSince(d)) >= gracePeriod()`; retire only running `d` with `newerDeployment(keep, d)` (`created_at`, then ID). `RetireOnlyOlder = FALSE` is the pre-F1 skip `d.ID == currentDeploymentID \|\| d.Status != running` |
| `ListByDeploymentID` | `retireSuperseded`: `containerRepo.ListByDeploymentID`, `c.Status == ContainerStatusRunning` |
| `StopContainer`, `StopContainerFails` | `retireContainer`: `dockerClient.StopContainer` (`internal/docker/container.go`) |
| `RemoveContainer`, `RemoveContainerFails`, `RemoveSkipped` | `retireContainer`: `dockerClient.RemoveContainer`, which uses `Force: true` (`internal/docker/container.go`); runs even if stop failed. A `cerrdefs.IsNotFound` error counts as removed. Any other error: return false, row left running, pass marked pending (`RetryFailedRetire`) |
| `UpdateStatusExited` | `retireContainer`: `containerRepo.UpdateStatus(ctx, c.ID, ContainerStatusExited)`, only after remove succeeded |
| `RetireSupersededReturns` | end of `retireSuperseded`: `errRetirePending` if any remove failed (`pruneImages` not modeled) |
| `RetryRetire` | `retireUntilDone`: on error, `waitGrace(ctx, retireRetryInterval())` (default 30 s) then the next pass, each pass under `context.WithTimeout(ctx, retireAttemptTimeout)` (2 min). Exits on success, ctx end, or current deployment no longer running (rollback, not modeled). Used by `CleanupOldDeployments` and the `RecoverPendingRetires` goroutine; `RunPipeline`'s cleanup goroutine has no overall timeout |
| `Restart` | process death: every goroutine (grace timers, retire loops) dies; DB rows and containers (`RestartUnlessStopped`) stay; `cmd/goforge/main.go` starts `go deployService.RecoverPendingRetires(backgroundCtx)` after `deployWorker.Start()` |
| `RecoverBoot` | `RecoverPendingRetires`: `ListByStatus(running)`, newest per env (`newerDeployment`) is current, `hasSuperseded`; then `wait := gracePeriod() - time.Since(runningSince(cur))`, `waitGrace`, `retireUntilDone` |
| `Newest` | `RecoverPendingRetires`' "newest running deployment of each environment is the current one" |

A change elsewhere (UI, rollback, Traefik labels, image build) does not need this check unless it changes deployment or container statuses.

**Sync commit:** `ea92e110376b2d96e23b27373eeb39b04525dcc7` (F1/F2 fix). The model was confirmed against the code above at this commit, and again after the soft-polish change that made `retireSuperseded` list running deployments with `ListByStatus` instead of `ListByEnvironmentID(env, 10)` (model action renamed to match; move the sync commit to that change's squash commit on `main`). To see whether covered code moved since then:

```bash
git diff --stat ea92e110376b2d96e23b27373eeb39b04525dcc7 -- internal/deploy/cleanup.go internal/deploy/pipeline.go cmd/goforge/main.go internal/docker/container.go internal/deploy/bluegreen.go internal/deploy/state.go
```

Move the sync commit forward only after you have read the changed code and confirmed every row of the coverage record still describes it (or updated the model and the row).

## Run

From the repo root:

```bash
formal/deploy-retire/check.sh
```

About 10 s. Needs `java` 11+ on `PATH` (or `JAVA=`) and, on first run, `curl` to fetch the pinned `tla2tools.jar` v1.7.4 (sha256-checked, cached in `~/.cache/goforge-formal/`; or set `TLA2TOOLS=`). TLC scratch goes to a temp dir outside the repo. Expected output ends with `all configs as expected`; any `FAIL` line exits 1 and keeps the TLC output path.

Then the Go side:

```bash
go test -count=1 -run 'TestRedeploy|TestCleanup|TestRecover' ./internal/deploy/            # must pass
go test -count=1 -run 'TestReplay|TestRetire' ./internal/deploy/                          # F1/F2 replays, must pass
```

**Read every TLC result with the pstack `/statechart` skill**, run on `DeployRetire.tla` with the config in question and `view.ts` as the view, output to a new folder outside the repo. It turns each broken rule into its shortest path in plain words and, for `DeployRetire.cfg` (under 500 states), draws the chart. Do not read raw TLC traces by hand. Never commit its output folder.

## Reading a result

- `ok ... other rules hold` and `... breaks as expected` everywhere: the change kept the model's verdicts. Still confirm the coverage record matches your diff.
- A rule breaks on `DeployRetire.cfg`, or a new rule breaks on another config: get the path from `/statechart`, then replay it against the real code as a Go test, one step per action (copy the pattern in `cleanup_replay_test.go`: `redeployFixture`, `restartFixture`, `hostRuntime`; put a new failing replay behind `//go:build formalreplay` until it is fixed). If the Go replay shows the same ghost or outage, it is a real bug: report it with the path in code words. If it does not, the model is wrong until shown otherwise: find the action that does not match the code and fix the model, not the rule.
- An expected break stops breaking: either the code fixed the finding (good: move the config's `expect-break:` line, drop the `formalreplay` tag on its replay test so CI keeps it fixed, note it in CHANGELOG) or the model lost the step that causes it (bad: fix the model).
- A mutant stops breaking: the model can no longer catch that bug. Fix the model.

Never delete or weaken an INVARIANT, never add a constraint, and never edit an `expect-break:` line just to get `ok`.

## Updating the model for an intended change

Change the action that stands for the code you changed, keep its name equal to the Go function, update its row in the coverage record, run `check.sh` and `/statechart`, and move the sync commit. If a Go step is split into two DB or Docker calls, split the action too: one action is one indivisible step in the code (one DB statement, one Docker API call, or pure in-memory work).

## Rules (INVARIANTs in DeployRetire.tla)

- `AfterGraceAtMostOneRunning`: once the newest deployment's grace is over and no cleanup or recovery goroutine is still working, at most one container of the environment is running.
- `NoGhostAfterRetire`: at that point every older running deployment's container is removed from the host, not merely stopped.
- `NewestKeepsRunning`: the newest running deployment's container is running and its row says running, in every state.
- `OldServesThroughGrace`: an older container stops only after some newer deployment's grace is over (blue-green keeps it serving).
- `TypeOK`, and no deadlock (`Done` is the stutter in the end state).

## Bounds and assumptions

- One environment. Deployment 1 is already running; up to 2 redeploys (BackToBack) or 1 (others); up to 2 restarts (main) or 1.
- Every redeploy succeeds (health check passes); failed, canceled, and rolled-back deployments are not modeled.
- Deploys reach running one at a time (one `Worker`). The pipeline tail is one step: a restart between `CreateContainer` and `Transition(running)` is not modeled.
- All deployments have the same grace period, so graces end in the order deployments reached running; `RecoverPendingRetires`' remaining wait ends when the original grace would have.
- `hasSuperseded`'s row reads are one step (reads only). The `retireRetry` wait is not timed: a retry may come at any point. `retireSuperseded` reads every running deployment of the environment (`ListByStatus`, no `LIMIT`), as the model's `RunningDeps` does; it used `ListByEnvironmentID(env, 10)` before, which could drop an older running deployment behind ten newer failed ones (`TestRetireSupersededPastTenNewerFailures`). `pruneImages` is not modeled. `DockerErrors` models an error with no effect; a partial effect is not modeled.
- DB writes do not fail. The DB clock (`finished_at`) and the goforge clock agree (`retireSuperseded` compares them for `keep`).

## Findings (fixed at the sync commit)

- **F1, `NewestKeepsRunning` (DeployRetireBackToBack.cfg; mutant PreF1RetireNewer.cfg).** Two redeploys within one grace period: deploy 2 reaches running, deploy 3 reaches running before deploy 2's grace ends; deploy 2's `CleanupOldDeployments` wakes and `retireSuperseded(env, dep2)` skipped only `dep2` and non-running deployments, so it retired deploy 3's container. Deploy 3's own cleanup then retired deploy 2: no container ran. Fixed: retire only deployments older than `keep`. Replay: `TestReplayBackToBackRedeployKeepsNewest`; also `TestRetireSupersededNeverRetiresNewer` (real grace).
- **F2, `AfterGraceAtMostOneRunning` and `NoGhostAfterRetire` (DeployRetireDockerErrors.cfg; mutant PreF2ExitOnError.cfg).** `StopContainer` and `RemoveContainer` both returned an error; `retireContainer` still ran `UpdateStatus(exited)`, so `hasSuperseded` and later cleanups skipped the row and the old container ran forever. Fixed: the row stays running until removed and `retireUntilDone` retries; after a restart `RecoverPendingRetires` finds it. Replay: `TestReplayDockerErrorsLeaveNoSecondRunning` (the restart is the ctx ending); also `TestCleanupOldDeploymentsRetriesFailedRemove`, `TestCleanupOldDeploymentsKeepsRowRunningWhileDockerDown`.

## Not covered

Rollback (`internal/deploy/rollback.go`, including a cleanup still retrying when a rollback makes an older deployment running again), failed or canceled deploys and their containers, restarts inside `RunPipeline`, several environments sharing cleanup, the `BlueGreenDeployer.scheduleCleanup` path (only used when `OldContainerID` is set, which `performHealthCheck` never does), Traefik routing, `pruneImages`, real Docker timing, and anything past the bounds above.

To bring this skill back in line after changes that skipped this check, use the pstack `/maintain-formal-verification-skill` skill.
