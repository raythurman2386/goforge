---------------------------- MODULE DeployRetire ----------------------------
(***************************************************************************)
(* Redeploy, blue-green grace, and retire of the previous container for    *)
(* ONE GoForge environment, including a goforge process restart.           *)
(*                                                                         *)
(* Code (sync commit in .cursor/skills/verify-goforge-deploy-retire):      *)
(*   internal/deploy/pipeline.go  RunPipeline tail: CreateContainer,       *)
(*       containerRepo.Create(running), Transition(running),               *)
(*       go CleanupOldDeployments                                          *)
(*   internal/deploy/cleanup.go   CleanupOldDeployments, waitGrace,        *)
(*       retireSuperseded, retireContainer, RecoverPendingRetires,         *)
(*       hasSuperseded, runningSince                                       *)
(*   cmd/goforge/main.go          go deployService.RecoverPendingRetires   *)
(*   internal/docker/container.go RemoveContainer uses Force: true         *)
(*                                                                         *)
(* Deployment 1 is the one already running before the first redeploy.    *)
(* Deployments 2..N are redeploys that reach running, in order (the       *)
(* deploy Worker runs one pipeline at a time).                            *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    N,               \* deployments 1..N of one environment
    MaxRestarts,     \* goforge process restarts allowed in a behavior
    RecoverOnBoot,   \* TRUE: main.go starts RecoverPendingRetires on boot (#96)
    RemoveOnRetire,  \* TRUE: retireContainer calls RemoveContainer after StopContainer (#95)
    DockerErrors     \* TRUE: StopContainer / RemoveContainer may return an error with no effect

ASSUME N \in 2..4 /\ MaxRestarts \in 0..2
ASSUME {RecoverOnBoot, RemoveOnRetire, DockerErrors} \subseteq BOOLEAN

Deps    == 1..N
Recover == N + 1          \* task id of the RecoverPendingRetires goroutine
TaskIds == 1..N + 1       \* task[d], d >= 2: CleanupOldDeployments(env, d). task[1] is never used.

VARIABLES
    dstatus,    \* deployments.status of d: "pending" | "running"
    row,        \* containers.status of d's container row: "none" | "running" | "exited"
    host,       \* d's Docker container on the host: "none" | "running" | "stopped" | "removed"
    graceOver,  \* graceOver[d]: gracePeriod has passed since runningSince(d)
    task,       \* in-memory goroutines (die on restart)
    restarts

vars == <<dstatus, row, host, graceOver, task, restarts>>

Idle   == [pc |-> "none", cur |-> 0, todo |-> {}, tgt |-> 0]
Active == {"boot", "waitGrace", "list", "next", "stop", "remove", "update"}

Max(S) == CHOOSE x \in S : \A y \in S : y <= x
RunningDeps == {d \in Deps : dstatus[d] = "running"}
\* RecoverPendingRetires: "The newest running deployment of each environment is the current one."
Newest == Max(RunningDeps)

Init ==
    /\ dstatus   = [d \in Deps |-> IF d = 1 THEN "running" ELSE "pending"]
    /\ row       = [d \in Deps |-> IF d = 1 THEN "running" ELSE "none"]
    /\ host      = [d \in Deps |-> IF d = 1 THEN "running" ELSE "none"]
    /\ graceOver = [d \in Deps |-> d = 1]
    /\ task      = [t \in TaskIds |-> Idle]
    /\ restarts  = 0

(* pipeline.go RunPipeline, success path after build: CreateContainer,    *)
(* containerRepo.Create(status running), performHealthCheck,               *)
(* Transition(running) (sets finished_at), then go CleanupOldDeployments. *)
(* One step: a restart inside the pipeline is out of scope (see skill).   *)
DeploymentRunning(d) ==
    /\ d >= 2
    /\ dstatus[d] = "pending"
    /\ dstatus[d - 1] = "running"
    /\ dstatus'   = [dstatus EXCEPT ![d] = "running"]
    /\ row'       = [row EXCEPT ![d] = "running"]
    /\ host'      = [host EXCEPT ![d] = "running"]
    /\ task'      = [task EXCEPT ![d] = [Idle EXCEPT !.pc = "waitGrace", !.cur = d]]
    /\ UNCHANGED <<graceOver, restarts>>

(* Wall-clock time: gracePeriod passes since runningSince(d). Every       *)
(* deployment has the same gracePeriod, so graces end in the order the    *)
(* deployments reached running.                                           *)
GraceElapses(d) ==
    /\ d >= 2
    /\ dstatus[d] = "running"
    /\ ~graceOver[d]
    /\ graceOver[d - 1]
    /\ graceOver' = [graceOver EXCEPT ![d] = TRUE]
    /\ UNCHANGED <<dstatus, row, host, task, restarts>>

(* waitGrace returns nil: the timer fired, or the wait was <= 0. For      *)
(* CleanupOldDeployments the wait is gracePeriod from now; for            *)
(* RecoverPendingRetires it is gracePeriod - time.Since(runningSince(cur)) *)
(* so both end when graceOver[cur].                                       *)
WaitGraceReturns(t) ==
    /\ task[t].pc = "waitGrace"
    /\ graceOver[task[t].cur]
    /\ task' = [task EXCEPT ![t].pc = "list"]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* retireSuperseded: deploymentRepo.ListByEnvironmentID(env, 10), then    *)
(* skip d.ID == currentDeploymentID || d.Status != running.               *)
(* NOTE: the code does not skip deployments NEWER than the current one.   *)
ListByEnvironmentID(t) ==
    /\ task[t].pc = "list"
    /\ task' = [task EXCEPT ![t].pc = "next",
                            ![t].todo = {d \in RunningDeps : d # task[t].cur}]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* Loop body: containerRepo.ListByDeploymentID(d.ID); a row with Status   *)
(* == running goes to retireContainer. Rows are visited created_at DESC.  *)
ListByDeploymentID(t) ==
    /\ task[t].pc = "next"
    /\ task[t].todo # {}
    /\ LET d == Max(task[t].todo) IN
         task' = [task EXCEPT ![t].todo = @ \ {d},
                              ![t].pc   = IF row[d] = "running" THEN "stop" ELSE "next",
                              ![t].tgt  = IF row[d] = "running" THEN d ELSE 0]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* Loop over: retireSuperseded returns; pruneImages is not modeled.       *)
RetireSupersededReturns(t) ==
    /\ task[t].pc = "next"
    /\ task[t].todo = {}
    /\ task' = [task EXCEPT ![t] = [Idle EXCEPT !.pc = "done", !.cur = task[t].cur]]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* retireContainer: dockerClient.StopContainer(c.DockerID). On a stopped  *)
(* or removed container it returns an error and changes nothing.          *)
StopContainer(t) ==
    /\ task[t].pc = "stop"
    /\ LET c == task[t].tgt IN
         host' = [host EXCEPT ![c] = IF @ = "running" THEN "stopped" ELSE @]
    /\ task' = [task EXCEPT ![t].pc = "remove"]
    /\ UNCHANGED <<dstatus, row, graceOver, restarts>>

StopContainerFails(t) ==
    /\ DockerErrors
    /\ task[t].pc = "stop"
    /\ task' = [task EXCEPT ![t].pc = "remove"]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* retireContainer: dockerClient.RemoveContainer (Force: true), run even  *)
(* if stop failed (#95). Removes a running or stopped container.          *)
RemoveContainer(t) ==
    /\ RemoveOnRetire
    /\ task[t].pc = "remove"
    /\ LET c == task[t].tgt IN
         host' = [host EXCEPT ![c] = IF @ \in {"running", "stopped"} THEN "removed" ELSE @]
    /\ task' = [task EXCEPT ![t].pc = "update"]
    /\ UNCHANGED <<dstatus, row, graceOver, restarts>>

RemoveContainerFails(t) ==
    /\ RemoveOnRetire
    /\ DockerErrors
    /\ task[t].pc = "remove"
    /\ task' = [task EXCEPT ![t].pc = "update"]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* Pre-#95 code: retireContainer stopped the container and never removed it. *)
RemoveSkipped(t) ==
    /\ ~RemoveOnRetire
    /\ task[t].pc = "remove"
    /\ task' = [task EXCEPT ![t].pc = "update"]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

(* retireContainer: containerRepo.UpdateStatus(c.ID, exited), whatever    *)
(* StopContainer and RemoveContainer returned.                            *)
UpdateStatusExited(t) ==
    /\ task[t].pc = "update"
    /\ row'  = [row EXCEPT ![task[t].tgt] = "exited"]
    /\ task' = [task EXCEPT ![t].pc = "next", ![t].tgt = 0]
    /\ UNCHANGED <<dstatus, host, graceOver, restarts>>

(* The goforge process dies and starts again. Every goroutine (grace      *)
(* timers, retire loops) is gone; DB rows and Docker containers stay      *)
(* (containers use RestartUnlessStopped). On boot main.go starts          *)
(* RecoverPendingRetires in a goroutine.                                  *)
Restart ==
    /\ restarts < MaxRestarts
    /\ task' = [t \in TaskIds |->
                  IF t = Recover /\ RecoverOnBoot THEN [Idle EXCEPT !.pc = "boot"]
                  ELSE IF task[t].pc \in Active THEN [task[t] EXCEPT !.pc = "killed"]
                  ELSE task[t]]
    /\ restarts' = restarts + 1
    /\ UNCHANGED <<dstatus, row, host, graceOver>>

(* RecoverPendingRetires up to the go func: ListByStatus(running), newest *)
(* per env is current, hasSuperseded reads the older deployments' rows.   *)
(* Reads only; done as one step.                                          *)
RecoverBoot ==
    /\ task[Recover].pc = "boot"
    /\ LET cur == Newest
           pending == \E d \in RunningDeps : d # cur /\ row[d] = "running"
       IN task' = [task EXCEPT ![Recover] =
                     IF pending THEN [Idle EXCEPT !.pc = "waitGrace", !.cur = cur]
                     ELSE [Idle EXCEPT !.pc = "done", !.cur = cur]]
    /\ UNCHANGED <<dstatus, row, host, graceOver, restarts>>

Settled == \A t \in TaskIds : task[t].pc \notin Active

(* End state: every redeploy ran, every grace passed, no goroutine left.  *)
Done ==
    /\ dstatus[N] = "running"
    /\ graceOver[N]
    /\ Settled
    /\ UNCHANGED vars

Next ==
    \/ \E d \in Deps : DeploymentRunning(d)
    \/ \E d \in Deps : GraceElapses(d)
    \/ \E t \in TaskIds : WaitGraceReturns(t)
    \/ \E t \in TaskIds : ListByEnvironmentID(t)
    \/ \E t \in TaskIds : ListByDeploymentID(t)
    \/ \E t \in TaskIds : RetireSupersededReturns(t)
    \/ \E t \in TaskIds : StopContainer(t)
    \/ \E t \in TaskIds : StopContainerFails(t)
    \/ \E t \in TaskIds : RemoveContainer(t)
    \/ \E t \in TaskIds : RemoveContainerFails(t)
    \/ \E t \in TaskIds : RemoveSkipped(t)
    \/ \E t \in TaskIds : UpdateStatusExited(t)
    \/ Restart
    \/ RecoverBoot
    \/ Done

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Rules. "Grace is over" means the newest running deployment's grace has *)
(* passed and no cleanup or recovery goroutine is still working.          *)
GraceOverAndSettled == graceOver[Newest] /\ Settled

TypeOK ==
    /\ dstatus \in [Deps -> {"pending", "running"}]
    /\ row \in [Deps -> {"none", "running", "exited"}]
    /\ host \in [Deps -> {"none", "running", "stopped", "removed"}]
    /\ graceOver \in [Deps -> BOOLEAN]
    /\ restarts \in 0..MaxRestarts

(* After the grace, at most one container of the environment is running. *)
AfterGraceAtMostOneRunning ==
    GraceOverAndSettled => Cardinality({d \in Deps : host[d] = "running"}) <= 1

(* After the grace, every older deployment's container is gone from the   *)
(* host (removed, not merely stopped).                                    *)
NoGhostAfterRetire ==
    GraceOverAndSettled =>
        \A d \in RunningDeps : d # Newest => host[d] = "removed"

(* The newest deployment's container is never retired: the site stays up. *)
NewestKeepsRunning ==
    host[Newest] = "running" /\ row[Newest] = "running"

(* The old container keeps serving until a newer deployment's grace ends. *)
OldServesThroughGrace ==
    \A d \in RunningDeps :
        (d # Newest /\ host[d] # "running") => \E e \in Deps : e > d /\ graceOver[e]
=============================================================================
