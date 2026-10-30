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

// PR4 dialog and confirm checks on the gallery, driven with the keyboard
// where a user would: native <dialog> focus trap and Escape, focus return,
// stacking, backdrop dismissal, in-dialog errors, and the kit confirm that
// replaces window.confirm(). Every request goes to the dev-only
// /dev/ui/dialog fragment route; nothing here touches app data.
async function dialogChecks(scheme, page) {
  const resp = await page.goto(BASE + "/dev/ui", { waitUntil: "load" });
  if (!resp || resp.status() !== 200) return;
  page.setDefaultTimeout(8000);
  let nativePrompts = 0;
  const onPrompt = (d) => {
    nativePrompts++;
    d.dismiss().catch(() => {});
  };
  page.on("dialog", onPrompt);
  let okRequests = 0;
  const onReq = (r) => {
    if (r.url().includes("/dev/ui/dialog?ok=1")) okRequests++;
  };
  page.on("request", onReq);
  const settle = () => page.waitForTimeout(300);
  const openDialogs = () => page.$$eval("dialog[open]", (ds) => ds.map((d) => d.id));
  const active = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      const d = a && a.closest("dialog");
      return { id: a ? a.id : "", tag: a ? a.tagName : "", dialog: d ? d.id : "", testid: a ? a.getAttribute("data-testid") || "" : "" };
    });
  const demo = 'dialog[data-testid="demo-dialog"]';
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));

  await t("dialog-open-focus", async () => {
    await page.focus("#gallery-dialog-open");
    await page.keyboard.press("Enter");
    await page.waitForSelector(demo + "[open]");
    await settle();
    const a = await active();
    const d = await page.$eval(demo, (el) => ({
      id: el.id,
      labelled: el.getAttribute("aria-labelledby"),
      title: (document.getElementById(el.getAttribute("aria-labelledby")) || {}).tagName || "",
      parent: el.parentElement.tagName,
    }));
    check(scheme, "dialog-open-focus", a.dialog === d.id && d.title === "H2" && d.parent === "BODY", `focus=${a.tag}#${a.id} in=${a.dialog} labelledby->${d.title} parent=${d.parent}`);
    let escaped = 0;
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press(i % 3 === 2 ? "Shift+Tab" : "Tab");
      const x = await active();
      if (x.dialog !== d.id && x.tag !== "BODY") escaped++;
    }
    check(scheme, "dialog-focus-trap", escaped === 0, `14 Tab/Shift+Tab presses, ${escaped} landed outside the dialog`);
    await page.keyboard.press("Escape");
    await settle();
    const gone = (await page.$(demo)) === null;
    const back = await active();
    check(scheme, "dialog-escape-focus-return", gone && back.id === "gallery-dialog-open", `removed=${gone} focus=#${back.id}`);
  });

  await t("dialog-stack", async () => {
    await page.click("#gallery-dialog-open");
    await page.waitForSelector(demo + "[open]");
    await page.click(demo + ' [data-testid="demo-stack"]');
    await page.waitForSelector(demo + '[data-depth="2"][open]');
    await settle();
    const ids = await openDialogs();
    const a = await active();
    const top = await page.$eval(demo + '[data-depth="2"]', (el) => el.id);
    await page.keyboard.press("Escape");
    await settle();
    const after = await openDialogs();
    const a2 = await active();
    await page.keyboard.press("Escape");
    await settle();
    const none = (await page.$$(demo)).length;
    check(
      scheme,
      "dialog-stack",
      ids.length === 2 && new Set(ids).size === 2 && a.dialog === top && after.length === 1 && a2.testid === "demo-stack" && none === 0,
      `open=${ids.length} unique=${new Set(ids).size} topFocused=${a.dialog === top} afterEsc=${after.length} focus=${a2.testid} finally=${none}`,
    );
  });

  await t("dialog-backdrop", async () => {
    await page.click("#gallery-dialog-open");
    await page.waitForSelector(demo + "[open]");
    await settle();
    await page.mouse.click(8, 8);
    await settle();
    const closed = (await page.$(demo)) === null;
    await page.click("#gallery-dialog-nondismissible");
    await page.waitForSelector(demo + "[open]");
    await settle();
    await page.mouse.click(8, 8);
    await settle();
    const stays = (await page.$(demo + "[open]")) !== null;
    await page.keyboard.press("Escape");
    await settle();
    const escClosed = (await page.$(demo)) === null;
    check(scheme, "dialog-backdrop", closed && stays && escClosed, `dismissible closed=${closed} nondismissible stays=${stays} then Escape closed=${escClosed}`);
  });

  await t("dialog-error-then-save", async () => {
    await page.click("#gallery-dialog-open");
    await page.waitForSelector(demo + "[open]");
    await page.click(demo + ' [data-testid="demo-fail"]');
    await page.waitForSelector(demo + " [data-ui-dialog-error]:not(.hidden)");
    const err = await page.$eval(demo + " [data-ui-dialog-error]", (el) => ({ text: el.textContent.trim(), role: el.getAttribute("role") }));
    const stillOpen = (await page.$(demo + "[open]")) !== null;
    const globalErr = (await page.$("#modal-error")) === null;
    const toast = await page.$$eval("#toast [role]", (els) => els.length).catch(() => 0);
    check(scheme, "dialog-error", stillOpen && err.role === "alert" && /Demo error/.test(err.text) && globalErr, `open=${stillOpen} role=${err.role} text="${err.text}" no#modal-error=${globalErr} toasts=${toast}`);
    await page.focus(demo + " input:not([type=hidden])");
    await page.keyboard.type("x");
    await page.keyboard.press("Enter");
    await page.waitForSelector(demo, { state: "detached", timeout: 3000 });
    await settle();
    const back = await active();
    check(scheme, "dialog-save-closes", back.id === "gallery-dialog-open", `Enter submitted (204), dialog removed, focus=#${back.id}`);
  });

  await t("dialog-static", async () => {
    await page.click("#gallery-static-open");
    await page.waitForSelector("#gallery-static-dialog[open]");
    await settle();
    await page.keyboard.press("Escape");
    await settle();
    const st = await page.$eval("#gallery-static-dialog", (el) => ({ open: el.open, connected: el.isConnected }));
    const back = await active();
    check(scheme, "dialog-static", !st.open && st.connected && back.id === "gallery-static-open", `open=${st.open} kept=${st.connected} focus=#${back.id}`);
  });

  await t("confirm-cancel", async () => {
    okRequests = 0;
    await page.focus("#gallery-confirm");
    await page.keyboard.press("Enter");
    await page.waitForSelector("#ui-confirm[open]");
    await settle();
    const c = await page.$eval("#ui-confirm", (el) => ({
      role: el.getAttribute("role"),
      msg: el.querySelector("[data-ui-confirm-message]").textContent.trim(),
      label: el.querySelector("[data-ui-confirm-accept]").textContent.trim(),
    }));
    const a = await active();
    await page.keyboard.press("Escape");
    await settle();
    const back = await active();
    const viaEsc = okRequests;
    await page.click("#gallery-confirm");
    await page.waitForSelector("#ui-confirm[open]");
    await page.click("#ui-confirm [data-ui-confirm-cancel]");
    await settle();
    check(
      scheme,
      "confirm-cancel",
      c.role === "alertdialog" && c.msg === "Send the demo request?" && c.label === "Send" && a.dialog === "ui-confirm" && back.id === "gallery-confirm" && okRequests === 0,
      `role=${c.role} msg="${c.msg}" accept="${c.label}" focus=${a.dialog} afterEsc focus=#${back.id} requests(Esc)=${viaEsc} requests(Cancel)=${okRequests}`,
    );
  });

  await t("confirm-accept-keyboard", async () => {
    okRequests = 0;
    await page.focus("#gallery-confirm");
    await page.keyboard.press("Enter");
    await page.waitForSelector("#ui-confirm[open]");
    await settle();
    await page.keyboard.press("Tab");
    const onAccept = await page.evaluate(() => document.activeElement.hasAttribute("data-ui-confirm-accept"));
    await page.keyboard.press("Enter");
    await page.waitForTimeout(600);
    const back = await active();
    check(scheme, "confirm-accept-keyboard", onAccept && okRequests === 1 && back.id === "gallery-confirm", `Tab->accept=${onAccept} requests=${okRequests} focus=#${back.id}`);
  });

  await t("confirm-danger", async () => {
    await page.click("#gallery-confirm-danger");
    await page.waitForSelector("#ui-confirm[open]");
    const acc = await page.$eval("#ui-confirm [data-ui-confirm-accept]", (el) => ({ cls: el.className, text: el.textContent.trim() }));
    await page.keyboard.press("Escape");
    await settle();
    check(scheme, "confirm-danger", acc.cls === "btn-danger" && acc.text === "Delete", `accept class=${acc.cls} label=${acc.text}`);
  });

  await t("dialog-axe", async () => {
    await page.click("#gallery-dialog-open");
    await page.waitForSelector(demo + "[open]");
    await settle();
    await page.evaluate(AXE_SOURCE);
    const run = (sel) =>
      page.evaluate(
        ([tags, sel]) => window.axe.run({ include: [[sel]] }, { runOnly: { type: "tag", values: tags }, resultTypes: ["violations"] }),
        [AXE_TAGS, sel],
      );
    const r1 = await run(demo);
    await page.keyboard.press("Escape");
    await settle();
    await page.click("#gallery-confirm");
    await page.waitForSelector("#ui-confirm[open]");
    await settle();
    const r2 = await run("#ui-confirm");
    await page.keyboard.press("Escape");
    await settle();
    // color-contrast is the known palette baseline (btn-primary 3.76:1),
    // reported by the page scans and out of scope here; gate the rest.
    const bad = (r) => r.violations.filter((v) => GATE_IMPACTS.has(v.impact) && v.id !== "color-contrast").map((v) => `${v.id}(${v.impact})`);
    const b = bad(r1).concat(bad(r2));
    const nodes = (r, id) => r.violations.filter((v) => v.id === id).reduce((n, v) => n + v.nodes.length, 0);
    const all = r1.violations.concat(r2.violations).map((v) => v.id);
    check(
      scheme,
      "dialog-axe",
      b.length === 0,
      `serious/critical other than color-contrast=${b.length}${b.length ? " " + b.join(",") : ""} color-contrast nodes dialog=${nodes(r1, "color-contrast")} confirm=${nodes(r2, "color-contrast")} all=[${all.join(",")}]`,
    );
  });

  await t("confirm-plain-form", async () => {
    await page.click("#gallery-confirm-form");
    await page.waitForSelector("#ui-confirm[open]");
    const before = new URL(page.url()).search;
    await page.keyboard.press("Escape");
    await settle();
    const unsent = new URL(page.url()).search === before;
    await page.click("#gallery-confirm-form");
    await page.waitForSelector("#ui-confirm[open]");
    await Promise.all([page.waitForURL(/confirmed=1/, { timeout: 5000 }), page.click("#ui-confirm [data-ui-confirm-accept]")]);
    check(scheme, "confirm-plain-form", unsent, `Escape kept url=${unsent} Confirm navigated to ${new URL(page.url()).search}`);
  });

  page.off("request", onReq);
  page.off("dialog", onPrompt);
  check(scheme, "confirm-no-native-prompt", nativePrompts === 0, `window.confirm prompts=${nativePrompts}`);
  page.setDefaultTimeout(30000);
}

// PR4 checks on real app dialogs (throwaway database only): the token
// dialog opens from its trigger, the form swaps its own dialog for the
// shown-once token dialog (backdrop-proof), focus returns to the trigger,
// and Revoke asks through the kit confirm; Cancel sends no DELETE.
async function appDialogChecks(scheme, page) {
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  page.setDefaultTimeout(8000);
  let deletes = 0;
  const onReq = (r) => {
    if (r.method() === "DELETE") deletes++;
  };
  let prompts = 0;
  const onPrompt = (d) => {
    prompts++;
    d.dismiss().catch(() => {});
  };
  page.on("request", onReq);
  page.on("dialog", onPrompt);
  const settle = () => page.waitForTimeout(300);
  const active = () => page.evaluate(() => ({ hx: document.activeElement.getAttribute("hx-get") || "", dialog: (document.activeElement.closest("dialog") || {}).id || "" }));
  await t("app-token-dialog", async () => {
    await page.goto(BASE + "/settings/profile", { waitUntil: "load" });
    const trigger = 'button[hx-get="/settings/tokens/new"]';
    await page.focus(trigger);
    await page.keyboard.press("Enter");
    await page.waitForSelector("dialog[data-ui-dialog][open] #token-name");
    await settle();
    const a = await active();
    await page.fill("#token-name", `ui-a11y-pr4-${scheme}`);
    await page.click('dialog[open] button[type="submit"]');
    await page.waitForSelector("dialog[data-ui-dialog-nondismissible][open]");
    await settle();
    const count = await page.$$eval("dialog[open]", (d) => d.length);
    await page.mouse.click(8, 8);
    await settle();
    const stays = (await page.$("dialog[data-ui-dialog-nondismissible][open]")) !== null;
    await page.keyboard.press("Escape");
    await settle();
    const gone = (await page.$('dialog[data-ui-dialog="ephemeral"]')) === null;
    const back = await active();
    check(
      scheme,
      "app-token-dialog",
      a.dialog.startsWith("dialog-") && count === 1 && stays && gone && back.hx === "/settings/tokens/new",
      `open focus in=${a.dialog} swapped-open=${count} backdrop-stays=${stays} esc-removed=${gone} focus-return=${back.hx}`,
    );
  });
  await t("app-revoke-confirm-cancel", async () => {
    await page.goto(BASE + "/settings/profile", { waitUntil: "load" });
    const revoke = "button[hx-delete^='/settings/tokens/']";
    await page.waitForSelector(revoke);
    deletes = 0;
    await page.click(revoke);
    await page.waitForSelector("#ui-confirm[open]");
    const label = await page.$eval("#ui-confirm [data-ui-confirm-accept]", (el) => el.textContent.trim() + "/" + el.className);
    await page.click("#ui-confirm [data-ui-confirm-cancel]");
    await settle();
    check(scheme, "app-revoke-confirm-cancel", deletes === 0 && label === "Revoke/btn-danger", `accept=${label} DELETE requests after Cancel=${deletes}`);
  });
  page.off("request", onReq);
  page.off("dialog", onPrompt);
  check(scheme, "app-no-native-prompt", prompts === 0, `window.confirm prompts=${prompts}`);
  page.setDefaultTimeout(30000);
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
      await dialogChecks(scheme, page);
      if (!custom) {
        if (canLogin && (await login(page))) {
          lines.push(`login ${scheme} pass`);
          await appDialogChecks(scheme, page);
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
