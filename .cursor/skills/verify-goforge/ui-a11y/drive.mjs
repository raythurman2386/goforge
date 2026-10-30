// verify.sh drive-ui-a11y: axe and keyboard checks in a real browser.
//
// verify.sh copies this file next to the pinned node_modules (package.json,
// package-lock.json) under /tmp/goforge-verify/ui-a11y-node and runs it there.
// Inputs come from the environment:
//   GOFORGE_UI_BASE           app base URL (verify.sh BASE)
//   GOFORGE_UI_EVIDENCE       evidence dir (outside the repo)
//   GOFORGE_UI_EMAIL          login email (lowercase admin email)
//   GOFORGE_UI_PASSWORD_FILE  password from drive-register; empty = public pages only
//   GOFORGE_UI_SCHEMES        default "light,dark"
//   GOFORGE_UI_ROUTES         optional comma list that replaces the route list
//   GOFORGE_VERIFY_CHROMIUM   optional Chromium executable path
//
// Writes ui-a11y-verdict.txt, ui-a11y-report.json, and screenshots under
// ui-a11y/<scheme>/. Never writes page HTML: axe node html snippets are
// dropped because pages embed the CSRF token.
// Exit 0 when the harness ran (product misses are fail lines), 2 when it
// could not run.
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { chromium } from "playwright-core";

const require = createRequire(import.meta.url);
const AXE_SOURCE = fs.readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");
const AXE_VERSION = require("axe-core/package.json").version;
const PW_VERSION = require("playwright-core/package.json").version;
const AXE_TAGS = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];
const GATE_IMPACTS = new Set(["serious", "critical"]);

const BASE = (process.env.GOFORGE_UI_BASE || "").replace(/\/$/, "");
const EVIDENCE = process.env.GOFORGE_UI_EVIDENCE;
const EMAIL = process.env.GOFORGE_UI_EMAIL || "";
const PASSWORD_FILE = process.env.GOFORGE_UI_PASSWORD_FILE || "";
const SCHEMES = (process.env.GOFORGE_UI_SCHEMES || "light,dark").split(",").map((s) => s.trim()).filter(Boolean);

const PUBLIC_ROUTES = ["/login", "/auth/register", "/dev/ui", "/ui-a11y-missing-page"];
const AUTH_ROUTES = [
  "/dashboard",
  "/projects",
  "/projects/new",
  "/services",
  "/containers",
  "/settings/profile",
  "/settings/git",
  "/ui-a11y-missing-page",
];
const PROJECT_TABS = ["", "/deployments", "/settings", "/services"];

const lines = [];
const report = { base: BASE, axe: AXE_VERSION, playwright: PW_VERSION, tags: AXE_TAGS, schemes: {}, checks: [] };
let axeGate = true;
let checksOK = true;

function slug(route) {
  return (route.replace(/^\//, "").replace(/[^a-z0-9]+/gi, "-") || "root").toLowerCase();
}

function writeFile(file, data) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, data);
  fs.chmodSync(file, 0o644);
}

// Strip html snippets (CSRF tokens live in hidden inputs) from axe nodes.
function scrubViolations(violations) {
  return violations.map((v) => ({
    id: v.id,
    impact: v.impact,
    help: v.help,
    helpUrl: v.helpUrl,
    tags: v.tags,
    nodes: v.nodes.map((n) => ({ target: n.target, impact: n.impact, failureSummary: n.failureSummary })),
  }));
}

function check(scheme, name, ok, detail) {
  if (!ok) checksOK = false;
  report.checks.push({ scheme, name, ok, detail });
  lines.push(`check ${scheme} ${name} ${ok ? "pass" : "fail"}${detail ? " " + detail : ""}`);
}

async function scan(page, scheme, route) {
  const resp = await page.goto(BASE + route, { waitUntil: "load" });
  const status = resp ? resp.status() : 0;
  if (route === "/dev/ui" && status === 404) {
    lines.push(`axe ${scheme} ${route} skip status=404 reason=GOFORGE_DEV_MODE-not-true`);
    return { route, status, skipped: true };
  }
  if (route === "/auth/register" && status === 403) {
    lines.push(`axe ${scheme} ${route} skip status=403 reason=registration-closed`);
    return { route, status, skipped: true };
  }
  // Polling and SSE pages never go network-idle; give htmx a moment instead.
  await page.waitForTimeout(400);
  await page.evaluate(AXE_SOURCE);
  const result = await page.evaluate(
    (tags) => window.axe.run(document, { runOnly: { type: "tag", values: tags }, resultTypes: ["violations"] }),
    AXE_TAGS,
  );
  const counts = { critical: 0, serious: 0, moderate: 0, minor: 0 };
  for (const v of result.violations) counts[v.impact] = (counts[v.impact] || 0) + 1;
  const gated = result.violations.filter((v) => GATE_IMPACTS.has(v.impact));
  if (gated.length) axeGate = false;
  const shot = path.join(EVIDENCE, "ui-a11y", scheme, slug(route) + ".png");
  await page.screenshot({ path: shot, fullPage: true });
  fs.chmodSync(shot, 0o644);
  const ids = gated.map((v) => `${v.id}(${v.impact},${v.nodes.length})`).join(",");
  lines.push(
    `axe ${scheme} ${route} ${gated.length ? "fail" : "pass"} status=${status} critical=${counts.critical} serious=${counts.serious} moderate=${counts.moderate} minor=${counts.minor}${ids ? " rules=" + ids : ""}`,
  );
  return { route, status, counts, violations: scrubViolations(result.violations), screenshot: shot };
}

// Foundation checks on the gallery: tokens follow the color scheme, rings
// show on keyboard focus only, reduced motion drops transitions, and the
// ui.js runtime initializes swapped markup once with delegated events.
async function galleryChecks(browser, scheme, page) {
  const resp = await page.goto(BASE + "/dev/ui", { waitUntil: "load" });
  if (!resp || resp.status() !== 200) {
    check(scheme, "gallery", false, `status=${resp ? resp.status() : 0} (needs GOFORGE_DEV_MODE=true)`);
    return;
  }
  const bg = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--gf-background").trim());
  const wantBg = scheme === "dark" ? "3 7 18" : "249 250 251";
  check(scheme, "token-scheme", bg === wantBg, `--gf-background="${bg}" want="${wantBg}"`);

  const shadow = (sel) => page.$eval(sel, (el) => getComputedStyle(el).boxShadow);
  const primary = "#buttons .btn-primary:not(.btn-sm)";
  const rest = await shadow(primary);
  await page.click("#buttons .btn-secondary:not([disabled])");
  await page.waitForTimeout(350);
  const afterMouse = await shadow("#buttons .btn-secondary:not([disabled])");
  await page.evaluate(() => document.activeElement && document.activeElement.blur());
  const secondaryRest = await shadow("#buttons .btn-secondary:not([disabled])");
  check(scheme, "focus-ring-mouse-hidden", afterMouse === secondaryRest, "mouse focus adds no ring");

  await page.focus("h1").catch(() => {});
  let reached = false;
  for (let i = 0; i < 60; i++) {
    await page.keyboard.press("Tab");
    reached = await page.evaluate((sel) => document.activeElement === document.querySelector(sel), primary);
    if (reached) break;
  }
  // .btn transitions box-shadow (transition-all, 200ms); read it settled.
  await page.waitForTimeout(350);
  const focused = reached ? await shadow(primary) : rest;
  check(scheme, "focus-ring-keyboard", reached && focused !== rest, reached ? (focused !== rest ? "Tab focus adds ring" : "Tab focus shows no ring") : "Tab never reached the primary button");

  const motion = await page.$eval(primary, (el) => parseFloat(getComputedStyle(el).transitionDuration));
  check(scheme, "motion-default", motion >= 0.1, `transition-duration=${motion}s`);
  const rctx = await browser.newContext({ colorScheme: scheme, reducedMotion: "reduce" });
  const rpage = await rctx.newPage();
  await rpage.goto(BASE + "/dev/ui", { waitUntil: "load" });
  const reduced = await rpage.$eval(primary, (el) => parseFloat(getComputedStyle(el).transitionDuration));
  check(scheme, "motion-reduced", reduced < 0.001, `transition-duration=${reduced}s`);
  await rctx.close();

  const runtime = await page.evaluate(() => {
    const ui = window.GoForgeUI;
    if (!ui || !window.htmx) return { ok: false, detail: "GoForgeUI or htmx missing" };
    let inits = 0;
    let clicks = 0;
    ui.register("probe", {
      init: () => {
        inits++;
      },
      events: {
        click: () => {
          clicks++;
        },
      },
    });
    const host = document.createElement("div");
    host.innerHTML = '<button type="button" data-ui="probe" id="ui-probe">probe</button>';
    document.body.appendChild(host);
    window.htmx.process(host);
    window.htmx.trigger(host, "htmx:load", { elt: host });
    ui.init(document);
    const first = inits;
    document.getElementById("ui-probe").click();
    // Simulate an htmx swap: replace the element, fire htmx:load again.
    host.innerHTML = '<button type="button" data-ui="probe" id="ui-probe">probe</button>';
    window.htmx.trigger(host, "htmx:load", { elt: host });
    document.getElementById("ui-probe").click();
    host.remove();
    return { ok: first === 1 && inits === 2 && clicks === 2, detail: `inits=${inits} clicks=${clicks} first-pass=${first}` };
  });
  check(scheme, "ui-js-delegation", runtime.ok, runtime.detail);
}

// Sign in by posting the real login form from the browser context, so the
// session cookie lands in the context's jar. drive-register stores the
// password with a trailing newline (curl --data-urlencode @file sends it
// as is), and a browser strips line breaks from password inputs, so typing
// it cannot work. The CSRF token is read from the form and never written out.
async function login(page) {
  await page.goto(BASE + "/login", { waitUntil: "load" });
  const csrf = await page.$eval('form[action="/auth/login"] input[name="csrf_token"]', (el) => el.value).catch(() => "");
  const resp = await page.request.post(BASE + "/auth/login", {
    form: { email: EMAIL, password: fs.readFileSync(PASSWORD_FILE, "utf8"), csrf_token: csrf },
    maxRedirects: 0,
  });
  if (resp.status() !== 303 || !/\/dashboard$/.test(resp.headers()["location"] || "")) return false;
  await page.goto(BASE + "/dashboard", { waitUntil: "load" });
  return new URL(page.url()).pathname === "/dashboard";
}

async function projectRoutes(page) {
  await page.goto(BASE + "/projects", { waitUntil: "load" });
  const href = await page.evaluate(() => {
    const re = /^\/projects\/[0-9a-f-]{36}$/;
    const a = Array.from(document.querySelectorAll("a[href]")).find((x) => re.test(x.getAttribute("href")));
    return a ? a.getAttribute("href") : null;
  });
  return href ? PROJECT_TABS.map((t) => href + t) : [];
}

async function main() {
  if (!BASE || !EVIDENCE) throw new Error("GOFORGE_UI_BASE and GOFORGE_UI_EVIDENCE are required");
  const launch = { headless: true };
  if (process.env.GOFORGE_VERIFY_CHROMIUM) launch.executablePath = process.env.GOFORGE_VERIFY_CHROMIUM;
  const browser = await chromium.launch(launch);
  const custom = process.env.GOFORGE_UI_ROUTES ? process.env.GOFORGE_UI_ROUTES.split(",").map((s) => s.trim()) : null;
  const canLogin = Boolean(PASSWORD_FILE && EMAIL && fs.existsSync(PASSWORD_FILE));
  try {
    for (const scheme of SCHEMES) {
      const results = [];
      const ctx = await browser.newContext({ colorScheme: scheme, viewport: { width: 1280, height: 900 } });
      const page = await ctx.newPage();
      for (const route of custom || PUBLIC_ROUTES) results.push(await scan(page, scheme, route));
      await galleryChecks(browser, scheme, page);
      if (!custom) {
        if (canLogin && (await login(page))) {
          lines.push(`login ${scheme} pass`);
          const routes = AUTH_ROUTES.concat(await projectRoutes(page));
          for (const route of routes) results.push(await scan(page, scheme, route));
        } else {
          lines.push(`login ${scheme} ${canLogin ? "fail" : "skip reason=no-register-password"}`);
        }
      }
      report.schemes[scheme] = results;
      await ctx.close();
    }
  } finally {
    await browser.close();
  }
  lines.push(`axe-gate=${axeGate ? "pass" : "fail"} (zero serious or critical across scanned pages)`);
  lines.push(`checks=${checksOK ? "pass" : "fail"}`);
  lines.push(`axe-core=${AXE_VERSION} playwright-core=${PW_VERSION}`);
  lines.push("harness=ok");
  writeFile(path.join(EVIDENCE, "ui-a11y-report.json"), JSON.stringify(report, null, 2) + "\n");
  writeFile(path.join(EVIDENCE, "ui-a11y-verdict.txt"), lines.join("\n") + "\n");
  console.log(lines.join("\n"));
}

main().catch((err) => {
  lines.push(`harness=fail reason=${String(err && err.message ? err.message : err).split("\n")[0]}`);
  try {
    writeFile(path.join(EVIDENCE, "ui-a11y-verdict.txt"), lines.join("\n") + "\n");
  } catch (e) {
    /* evidence dir unusable */
  }
  console.error(lines.join("\n"));
  process.exit(2);
});
