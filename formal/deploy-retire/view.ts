// Reader view of DeployRetire.tla for the pstack /statechart chart.
// Labels say what an operator sees on the Docker host and in GoForge.

type Task = { pc: string; cur: number; todo: unknown; tgt: number };
type Vars = {
  dstatus: Record<string, string>;
  row: Record<string, string>;
  host: Record<string, string>;
  graceOver: Record<string, boolean>;
  task: Record<string, Task>;
  restarts: number;
};

const ids = (o: Record<string, unknown>) => Object.keys(o).map(Number).sort((a, b) => a - b);
const tasks = (v: Vars) => ids(v.task).map((t) => ({ id: t, ...v.task[t] }));
const recoverId = (v: Vars) => ids(v.task).length; // N + 1
const newest = (v: Vars) => Math.max(...ids(v.dstatus).filter((d) => v.dstatus[d] === "running"));
const ACTIVE = new Set(["waitGrace", "list", "next", "stop", "remove", "update"]);

const hostWords = (v: Vars) => {
  const n = newest(v);
  const parts: string[] = [];
  for (const d of ids(v.host)) {
    const h = v.host[d];
    if (h === "none" || h === "removed") continue;
    const who = d === n ? `new deploy ${d}` : `old deploy ${d}`;
    parts.push(`${who} ${h}`);
  }
  return parts.length ? parts.join(", ") : "no container";
};

// One phase word per model state: what GoForge is doing about the old container.
const phase = (v: Vars) => {
  const ts = tasks(v);
  const rec = v.task[recoverId(v)];
  if (rec.pc === "boot" || ACTIVE.has(rec.pc)) return "recovering after restart";
  if (ts.some((t) => ACTIVE.has(t.pc) && (t.pc !== "waitGrace" || v.graceOver[t.cur]))) return "retiring";
  if (ts.some((t) => t.pc === "waitGrace")) return "in grace";
  if (!v.graceOver[newest(v)]) return "in grace, no cleanup pending";
  return "settled";
};

export const state = (v: Vars) => `${hostWords(v)} (${phase(v)})`;

export const group = (v: Vars) => {
  const p = phase(v);
  if (p === "recovering after restart") return "recover";
  if (p === "settled") return "settled";
  if (p === "retiring") return "retire";
  if (p === "in grace, no cleanup pending") return "orphan";
  return "grace";
};

export const colors = { grace: "blue", retire: "orange", recover: "purple", settled: "teal", orphan: "pink" };

export const title = "GoForge redeploy: blue-green grace and retire of the previous container (one environment)";

export const step = (action: string, from: Vars, to: Vars): string => {
  const a = action.replace(/\(.*$/, "");
  switch (a) {
    case "DeploymentRunning":
      return "a redeploy reaches running";
    case "GraceElapses":
      return "the grace period passes";
    case "WaitGraceReturns":
      return "cleanup wakes after the grace";
    case "ListByEnvironmentID":
      return "cleanup lists running deployments";
    case "ListByDeploymentID":
      return "cleanup reads a container row";
    case "RetireSupersededReturns":
      return "cleanup finishes";
    case "StopContainer":
      return "docker stop";
    case "StopContainerFails":
      return "docker stop fails";
    case "RemoveContainer":
      return "docker rm";
    case "RemoveContainerFails":
      return "docker rm fails";
    case "RemoveSkipped":
      return "remove is skipped";
    case "UpdateStatusExited":
      return "row marked exited";
    case "Restart":
      return "goforge restarts";
    case "RecoverBoot": {
      const rec = to.task[recoverId(to)];
      return rec.pc === "done" ? "recovery finds nothing to retire" : "recovery finds a pending retire";
    }
    case "Done":
      return "nothing left to do";
    default:
      return a;
  }
};
