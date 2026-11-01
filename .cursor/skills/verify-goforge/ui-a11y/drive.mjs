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
//   GOFORGE_UI_SERVICE_ID     optional service instance id (else first on /services)
//   GOFORGE_UI_CONTAINER_ID   optional docker id (else first on /containers)
//   GOFORGE_UI_DEPLOYMENT_PATH optional /projects/{id}/deployments/{id} path
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
// /settings/profile also carries the backup restore points (#104/#105) and
// the notifier form with its SMTP fields (#106/#109). Service, container,
// and deployment detail routes are added per run (detailIDs below).
const AUTH_ROUTES = [
  "/dashboard",
  "/projects",
  "/projects/new",
  "/services",
  "/services/postgres/configure",
  "/containers",
  "/settings/profile",
  "/settings/git",
  "/settings/activity",
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
  // Let finite entrance animations end first: #dashboard-stats fades in for
  // 0.5s, and a scan mid-fade reads its labels at about 4.3:1. Infinite ones
  // (the spinner) are skipped; the wait is capped at 2s.
  await page.evaluate(() =>
    Promise.race([
      Promise.all(
        document
          .getAnimations()
          .filter((a) => Number.isFinite(a.effect && a.effect.getComputedTiming().endTime))
          .map((a) => a.finished.catch(() => {})),
      ),
      new Promise((r) => setTimeout(r, 2000)),
    ]),
  );
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
  const wantBg = scheme === "dark" ? "20 20 20" : "238 238 238";
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
    // The palette baseline is cleared, so every serious or critical rule
    // gates here, color-contrast included.
    const bad = (r) => r.violations.filter((v) => GATE_IMPACTS.has(v.impact)).map((v) => `${v.id}(${v.impact})`);
    const b = bad(r1).concat(bad(r2));
    const nodes = (r, id) => r.violations.filter((v) => v.id === id).reduce((n, v) => n + v.nodes.length, 0);
    const all = r1.violations.concat(r2.violations).map((v) => v.id);
    check(
      scheme,
      "dialog-axe",
      b.length === 0,
      `serious/critical=${b.length}${b.length ? " " + b.join(",") : ""} color-contrast nodes dialog=${nodes(r1, "color-contrast")} confirm=${nodes(r2, "color-contrast")} all=[${all.join(",")}]`,
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

// PR5 overlay checks on the gallery: Dropdown, Popover, and Tooltip on the
// popover attribute, driven with the keyboard and the mouse. Menu actions
// go only to the dev-only /dev/ui/dialog route.
async function overlayChecks(scheme, page) {
  const resp = await page.goto(BASE + "/dev/ui", { waitUntil: "load" });
  if (!resp || resp.status() !== 200) return;
  page.setDefaultTimeout(8000);
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  let okRequests = 0;
  const onReq = (r) => {
    if (r.url().includes("/dev/ui/dialog?ok=1")) okRequests++;
  };
  page.on("request", onReq);
  let prompts = 0;
  const onPrompt = (d) => {
    prompts++;
    d.dismiss().catch(() => {});
  };
  page.on("dialog", onPrompt);
  const settle = (ms = 200) => page.waitForTimeout(ms);
  const isOpen = (id) => page.$eval("#" + id, (el) => el.matches(":popover-open"));
  const expanded = (id) => page.$eval("#" + id + "-trigger", (el) => el.getAttribute("aria-expanded"));
  const act = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      return { id: a.id || "", testid: a.getAttribute("data-testid") || "", text: (a.textContent || "").trim(), role: a.getAttribute("role") || "", tag: a.tagName };
    });
  const rect = (sel) => page.$eval(sel, (el) => el.getBoundingClientRect().toJSON());
  const key = async (k) => {
    await page.keyboard.press(k);
    await settle(80);
    return (await act()).text;
  };
  const M = "gallery-menu";

  await t("dropdown-keyboard", async () => {
    await page.focus("#" + M + "-trigger");
    await page.keyboard.press("Enter");
    await settle();
    const open = await isOpen(M);
    const exp = await expanded(M);
    const first = await act();
    const seq = [];
    for (const k of ["ArrowDown", "ArrowDown", "End", "Home", "ArrowUp", "ArrowDown"]) seq.push(await key(k));
    const ta = [];
    for (const k of ["d", "d", "o"]) {
      await page.waitForTimeout(600);
      ta.push(await key(k));
    }
    await page.keyboard.press("Escape");
    await settle();
    const closed = !(await isOpen(M));
    const exp2 = await expanded(M);
    const back = await act();
    const wantSeq = ["Duplicate", "Deploy", "Delete", "Overview", "Delete", "Overview"];
    const wantTa = ["Duplicate", "Deploy", "Open dialog…"];
    const ok =
      open && exp === "true" && first.role === "menuitem" && first.text === "Overview" &&
      seq.join("|") === wantSeq.join("|") && ta.join("|") === wantTa.join("|") &&
      closed && exp2 === "false" && back.id === M + "-trigger";
    check(scheme, "dropdown-keyboard", ok, `Enter open=${open} aria-expanded=${exp} focus=${first.role}:${first.text} keys=[${seq.join(",")}] typeahead=[${ta.join(",")}] Escape closed=${closed} aria-expanded=${exp2} focus=#${back.id}`);
  });

  await t("dropdown-open-keys", async () => {
    await page.focus("#" + M + "-trigger");
    await page.keyboard.press("ArrowUp");
    await settle();
    const up = (await act()).text;
    await page.keyboard.press("Escape");
    await settle();
    await page.keyboard.press("ArrowDown");
    await settle();
    const down = (await act()).text;
    await page.keyboard.press("Escape");
    await settle();
    await page.keyboard.press("Space");
    await settle();
    const space = (await act()).text;
    const spaceOpen = await isOpen(M);
    await page.keyboard.press("Tab");
    await settle();
    const tabClosed = !(await isOpen(M));
    const afterTab = await act();
    const ok = up === "Delete" && down === "Overview" && space === "Overview" && spaceOpen && tabClosed && afterTab.id === M + "-icon-trigger";
    check(scheme, "dropdown-open-keys", ok, `ArrowUp->${up} ArrowDown->${down} Space->${space} Tab closed=${tabClosed} focus=#${afterTab.id}`);
  });

  await t("dropdown-placement", async () => {
    await page.$eval("#" + M + "-trigger", (el) => el.scrollIntoView({ block: "center" }));
    await page.click("#" + M + "-trigger");
    await settle(300);
    const tr = await rect("#" + M + "-trigger");
    const mr = await rect("#" + M);
    const placed = await page.$eval("#" + M, (el) => el.getAttribute("data-ui-placed"));
    const focus = await act();
    await page.keyboard.press("Escape");
    await settle();
    const below = placed === "bottom" && mr.top >= tr.bottom && mr.top - tr.bottom <= 10 && Math.abs(mr.left - tr.left) <= 1;
    // Flip: put the trigger at the bottom edge of the viewport.
    await page.$eval("#" + M + "-trigger", (el) => el.scrollIntoView({ block: "end" }));
    await page.click("#" + M + "-trigger");
    await settle(300);
    const tr2 = await rect("#" + M + "-trigger");
    const mr2 = await rect("#" + M);
    const placed2 = await page.$eval("#" + M, (el) => el.getAttribute("data-ui-placed"));
    await page.keyboard.press("Escape");
    await settle();
    const vh = await page.evaluate(() => document.documentElement.clientHeight);
    const flipped = placed2 === "top" && mr2.bottom <= tr2.top && mr2.top >= 0 && tr2.bottom <= vh;
    // End alignment on the icon menu.
    await page.$eval("#" + M + "-icon-trigger", (el) => el.scrollIntoView({ block: "center" }));
    await page.click("#" + M + "-icon-trigger");
    await settle(300);
    const ir = await rect("#" + M + "-icon-trigger");
    const imr = await rect("#" + M + "-icon");
    await page.keyboard.press("Escape");
    await settle();
    const endAligned = Math.abs(imr.right - ir.right) <= 1;
    check(
      scheme,
      "dropdown-placement",
      below && flipped && endAligned && focus.role === "menuitem",
      `below=${below} (gap=${Math.round(mr.top - tr.bottom)}px) flip-top=${flipped} (menu.bottom=${Math.round(mr2.bottom)} trigger.top=${Math.round(tr2.top)}) end-aligned=${endAligned} click-open focus=${focus.role}`,
    );
  });

  await t("dropdown-light-dismiss", async () => {
    await page.$eval("#" + M + "-trigger", (el) => el.scrollIntoView({ block: "center" }));
    await page.click("#" + M + "-trigger");
    await settle();
    const open = await isOpen(M);
    await page.mouse.click(5, 5);
    await settle();
    const closed = !(await isOpen(M));
    const exp = await expanded(M);
    await page.click("#" + M + "-trigger");
    await settle();
    await page.click("#" + M + "-trigger");
    await settle();
    const toggled = !(await isOpen(M));
    check(scheme, "dropdown-light-dismiss", open && closed && exp === "false" && toggled, `open=${open} outside-click closed=${closed} aria-expanded=${exp} trigger re-click closes=${toggled}`);
  });

  await t("dropdown-item-request", async () => {
    okRequests = 0;
    await page.focus("#" + M + "-trigger");
    await page.keyboard.press("Enter");
    await settle();
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Enter");
    await settle(500);
    const closed = !(await isOpen(M));
    const back = await act();
    check(scheme, "dropdown-item-request", okRequests === 1 && closed && back.id === M + "-trigger", `Enter on Duplicate requests=${okRequests} menu closed=${closed} focus=#${back.id}`);
  });

  await t("dropdown-dialog-return", async () => {
    await page.focus("#" + M + "-trigger");
    await page.keyboard.press("Enter");
    await settle();
    await page.keyboard.press("o");
    const item = (await act()).text;
    await page.keyboard.press("Enter");
    await page.waitForSelector('dialog[data-ui-dialog="ephemeral"][open]');
    await settle(300);
    const inDialog = await page.evaluate(() => !!document.activeElement.closest("dialog[open]"));
    const menuClosed = !(await isOpen(M));
    await page.keyboard.press("Escape");
    await settle(300);
    const gone = (await page.$('dialog[data-ui-dialog="ephemeral"]')) === null;
    const back = await act();
    check(scheme, "dropdown-dialog-return", item === "Open dialog…" && inDialog && menuClosed && gone && back.id === M + "-trigger", `item=${item} focus-in-dialog=${inDialog} menu closed=${menuClosed} Escape removed=${gone} focus=#${back.id}`);
  });

  await t("dropdown-confirm", async () => {
    okRequests = 0;
    await page.focus("#" + M + "-trigger");
    await page.keyboard.press("ArrowUp");
    await settle();
    const item = (await act()).text;
    await page.keyboard.press("Enter");
    await page.waitForSelector("#ui-confirm[open]");
    await settle();
    const label = await page.$eval("#ui-confirm [data-ui-confirm-accept]", (el) => el.textContent.trim());
    await page.keyboard.press("Escape");
    await settle(300);
    const back = await act();
    check(scheme, "dropdown-confirm", item === "Delete" && label === "Delete" && okRequests === 0 && back.id === M + "-trigger", `item=${item} confirm label=${label} requests after Escape=${okRequests} focus=#${back.id}`);
  });

  const P = "gallery-popover";
  await t("popover", async () => {
    await page.focus("#" + P + "-trigger");
    await page.keyboard.press("Enter");
    await settle();
    const open = await isOpen(P);
    const exp = await expanded(P);
    const inside = await page.evaluate((id) => document.getElementById(id).contains(document.activeElement), P);
    const role = await page.$eval("#" + P, (el) => el.getAttribute("role") + "/" + document.getElementById(el.getAttribute("aria-labelledby")).textContent.trim());
    await page.keyboard.press("Escape");
    await settle();
    const escClosed = !(await isOpen(P));
    const back1 = (await act()).id;
    await page.click("#" + P + "-trigger");
    await settle();
    await page.mouse.click(5, 5);
    await settle();
    const lightClosed = !(await isOpen(P));
    await page.click("#" + P + "-trigger");
    await settle();
    await page.click('[data-testid="popover-done"]');
    await settle();
    const doneClosed = !(await isOpen(P));
    const back2 = (await act()).id;
    const exp2 = await expanded(P);
    const ok = open && exp === "true" && inside && role === "dialog/About overlays" && escClosed && back1 === P + "-trigger" && lightClosed && doneClosed && back2 === P + "-trigger" && exp2 === "false";
    check(scheme, "popover", ok, `Enter open=${open} aria-expanded=${exp} focus-inside=${inside} ${role} Escape closed=${escClosed} focus=#${back1} outside-click closed=${lightClosed} Got-it closed=${doneClosed} focus=#${back2}`);
  });

  const TIP = "gallery-tip-restart";
  await t("tooltip-focus", async () => {
    await page.mouse.move(2, 2);
    await page.focus("#gallery-tip-trigger");
    await page.waitForTimeout(100);
    const early = await isOpen(TIP);
    await page.waitForTimeout(400);
    const shown = await isOpen(TIP);
    const wiring = await page.$eval("#gallery-tip-trigger", (el) => el.getAttribute("aria-describedby") + "/" + el.getAttribute("aria-label"));
    const role = await page.$eval("#" + TIP, (el) => el.getAttribute("role"));
    const tr = await rect("#gallery-tip-trigger");
    const tp = await rect("#" + TIP);
    const above = tp.bottom <= tr.top && Math.abs(tp.left + tp.width / 2 - (tr.left + tr.width / 2)) <= 1;
    await page.keyboard.press("Escape");
    await settle();
    const hidden = !(await isOpen(TIP));
    const stay = (await act()).id;
    const ok = !early && shown && role === "tooltip" && wiring === TIP + "/Restart web" && above && hidden && stay === "gallery-tip-trigger";
    check(scheme, "tooltip-focus", ok, `at100ms=${early} at500ms=${shown} role=${role} describedby/label=${wiring} above-centered=${above} Escape hidden=${hidden} focus stays=#${stay}`);
    await page.evaluate(() => document.activeElement.blur());
  });

  await t("tooltip-hover", async () => {
    await page.mouse.move(2, 2);
    await settle(300);
    const tr = await rect("#gallery-tip-trigger");
    await page.mouse.move(tr.left + tr.width / 2, tr.top + tr.height / 2, { steps: 3 });
    await page.waitForTimeout(500);
    const shown = await isOpen(TIP);
    const tp = await rect("#" + TIP);
    await page.mouse.move(tp.left + tp.width / 2, tp.top + tp.height / 2, { steps: 4 });
    await page.waitForTimeout(400);
    const hoverable = await isOpen(TIP);
    await page.mouse.move(2, 2, { steps: 3 });
    await page.waitForTimeout(400);
    const left = !(await isOpen(TIP));
    check(scheme, "tooltip-hover", shown && hoverable && left, `hover shows=${shown} pointer on tooltip keeps it=${hoverable} pointer away hides=${left}`);
  });

  await t("tooltip-escape-in-menu-trigger", async () => {
    // A tooltip on a menu trigger: Escape hides the tooltip only; the icon
    // trigger keeps its aria-label as the name.
    await page.focus("#" + M + "-icon-trigger");
    await page.waitForTimeout(500);
    const shown = await isOpen("gallery-tip-more");
    const name = await page.$eval("#" + M + "-icon-trigger", (el) => el.getAttribute("aria-label"));
    await page.keyboard.press("Escape");
    await settle();
    const hidden = !(await isOpen("gallery-tip-more"));
    const stay = (await act()).id;
    check(scheme, "tooltip-icon-name", shown && hidden && name === "More actions" && stay === M + "-icon-trigger", `tooltip shown=${shown} Escape hidden=${hidden} aria-label=${name} focus=#${stay}`);
    await page.evaluate(() => document.activeElement.blur());
  });

  await t("overlays-history", async () => {
    await page.click("#" + M + "-trigger");
    await settle();
    const before = await isOpen(M);
    await page.evaluate(() => document.body.dispatchEvent(new CustomEvent("htmx:beforeHistorySave", { bubbles: true })));
    await settle();
    const after = await isOpen(M);
    const open = await page.$$eval("[data-ui-overlay]", (els) => els.filter((e) => e.matches(":popover-open")).length);
    check(scheme, "overlays-history", before && !after && open === 0, `menu open=${before} after htmx:beforeHistorySave open=${after} overlays open=${open}`);
  });

  await t("overlays-axe", async () => {
    await page.evaluate(AXE_SOURCE);
    const run = (sel) =>
      page.evaluate(
        ([tags, sel]) => window.axe.run({ include: [[sel]] }, { runOnly: { type: "tag", values: tags }, resultTypes: ["violations"] }),
        [AXE_TAGS, sel],
      );
    await page.click("#" + M + "-trigger");
    await settle(300);
    const r1 = await run("#" + M);
    await page.keyboard.press("Escape");
    await settle();
    await page.click("#" + P + "-trigger");
    await settle(300);
    const r2 = await run("#" + P);
    await page.keyboard.press("Escape");
    await settle();
    await page.focus("#gallery-tip-trigger");
    await page.waitForTimeout(500);
    const r3 = await run("#overlays");
    await page.keyboard.press("Escape");
    const bad = (r) => r.violations.filter((v) => GATE_IMPACTS.has(v.impact)).map((v) => `${v.id}(${v.impact})`);
    const b = bad(r1).concat(bad(r2), bad(r3));
    const all = r1.violations.concat(r2.violations, r3.violations).map((v) => `${v.id}(${v.impact})`);
    check(scheme, "overlays-axe", b.length === 0, `serious/critical=${b.length}${b.length ? " " + b.join(",") : ""} all=[${all.join(",")}]`);
  });

  page.off("request", onReq);
  page.off("dialog", onPrompt);
  check(scheme, "overlays-no-native-prompt", prompts === 0, `window.confirm prompts=${prompts}`);
  page.setDefaultTimeout(30000);
}

// PR5 on the real project page: the EnvironmentSwitcher is a kit Dropdown.
async function appEnvMenuChecks(scheme, page, projectHref) {
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  if (!projectHref) {
    check(scheme, "app-env-menu", false, "no project to open (run drive-login first)");
    return;
  }
  page.setDefaultTimeout(8000);
  const settle = (ms = 200) => page.waitForTimeout(ms);
  const act = () => page.evaluate(() => ({ id: document.activeElement.id, text: document.activeElement.textContent.trim(), role: document.activeElement.getAttribute("role") || "" }));
  const isOpen = () => page.$eval("#env-menu", (el) => el.matches(":popover-open"));
  await t("app-env-menu", async () => {
    await page.goto(BASE + projectHref, { waitUntil: "load" });
    const legacy = await page.$$eval('#menu-button, [aria-expanded="true"]', (els) => els.length);
    const exp0 = await page.$eval("#env-menu-trigger", (el) => el.getAttribute("aria-expanded") + "/" + el.getAttribute("aria-haspopup"));
    await page.focus("#env-menu-trigger");
    await page.keyboard.press("Enter");
    await settle();
    const open = await isOpen();
    const first = await act();
    await page.keyboard.press("End");
    await settle(80);
    const last = await act();
    await page.keyboard.press("Escape");
    await settle();
    const closed = !(await isOpen());
    const back = await act();
    const exp1 = await page.$eval("#env-menu-trigger", (el) => el.getAttribute("aria-expanded"));
    const ok = legacy === 0 && exp0 === "false/menu" && open && first.role === "menuitem" && last.text.endsWith("New Environment") && closed && back.id === "env-menu-trigger" && exp1 === "false";
    check(scheme, "app-env-menu", ok, `legacy #menu-button/aria-expanded=true=${legacy} initial=${exp0} Enter open=${open} first=${first.role}:${first.text} End=${last.text} Escape closed=${closed} focus=#${back.id} aria-expanded=${exp1}`);
  });
  await t("app-env-new-dialog", async () => {
    await page.focus("#env-menu-trigger");
    await page.keyboard.press("ArrowUp");
    await settle();
    await page.keyboard.press("Enter");
    await page.waitForSelector('dialog[data-ui-dialog="ephemeral"][open] #environment-name');
    await settle(300);
    const focus = await act();
    await page.keyboard.press("Escape");
    await settle(300);
    const back = await act();
    check(scheme, "app-env-new-dialog", focus.id === "environment-name" && back.id === "env-menu-trigger", `New Environment dialog focus=#${focus.id} Escape focus=#${back.id}`);
  });
  await t("app-env-link", async () => {
    await page.focus("#env-menu-trigger");
    await page.keyboard.press("ArrowDown");
    await settle();
    await Promise.all([page.waitForURL(/[?&]env=/, { timeout: 5000 }), page.keyboard.press("Enter")]);
    const url = new URL(page.url());
    check(scheme, "app-env-link", url.searchParams.has("env"), `Enter on first environment navigated to ${url.pathname}?env=…`);
  });
  page.setDefaultTimeout(30000);
}

// PR6 checks on the gallery: in-page Tabs (roving tabindex, arrows wrap,
// Home/End, automatic activation), Table scroll regions, and both Sheets
// (static dialog on an edge: trap, Escape, focus return, aria-expanded).
async function tabsTableSheetChecks(scheme, page) {
  const resp = await page.goto(BASE + "/dev/ui", { waitUntil: "load" });
  if (!resp || resp.status() !== 200) return;
  page.setDefaultTimeout(8000);
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  const settle = () => page.waitForTimeout(300);
  const state = () =>
    page.evaluate(() => {
      const root = document.getElementById("gallery-tabs");
      const tabs = Array.from(root.querySelectorAll('[role="tab"]'));
      const panels = Array.from(root.querySelectorAll('[role="tabpanel"]'));
      return {
        focus: document.activeElement.getAttribute("data-ui-tab") || document.activeElement.id,
        selected: tabs.filter((x) => x.getAttribute("aria-selected") === "true").map((x) => x.dataset.uiTab).join(","),
        zero: tabs.filter((x) => x.tabIndex === 0).map((x) => x.dataset.uiTab).join(","),
        shown: panels.filter((p) => !p.hidden).map((p) => p.dataset.uiTabpanel).join(","),
      };
    });
  await t("tabs-keyboard", async () => {
    await page.focus("#gallery-tabs-tab-build");
    const steps = [];
    const want = [
      ["ArrowRight", "runtime"],
      ["ArrowRight", "events"],
      ["ArrowRight", "build"],
      ["ArrowLeft", "events"],
      ["Home", "build"],
      ["End", "events"],
    ];
    let ok = true;
    for (const [key, exp] of want) {
      await page.keyboard.press(key);
      const s = await state();
      const good = s.focus === exp && s.selected === exp && s.zero === exp && s.shown === exp;
      if (!good) ok = false;
      steps.push(`${key}->${s.focus}/${s.selected}/${s.shown}${good ? "" : "!"}`);
    }
    // Tab leaves the tablist for the active panel (roving tabindex).
    await page.keyboard.press("Tab");
    const out = await page.evaluate(() => document.activeElement.getAttribute("role") + ":" + (document.activeElement.dataset.uiTabpanel || ""));
    if (out !== "tabpanel:events") ok = false;
    check(scheme, "tabs-keyboard", ok, steps.join(" ") + ` Tab->${out}`);
  });
  await t("tabs-click", async () => {
    await page.click("#gallery-tabs-tab-runtime");
    const s = await state();
    check(scheme, "tabs-click", s.selected === "runtime" && s.shown === "runtime", `selected=${s.selected} shown=${s.shown}`);
  });
  await t("table-scroll-regions", async () => {
    const r = await page.$$eval("[data-ui-table]", (ws) =>
      ws.map((w) => ({
        role: w.getAttribute("role"),
        tab: w.tabIndex,
        name: (document.getElementById(w.getAttribute("aria-labelledby")) || {}).textContent || "",
        caption: !!w.querySelector("table > caption"),
      })),
    );
    const bad = r.filter((x) => x.role !== "region" || x.tab !== 0 || !x.name.trim() || !x.caption);
    check(scheme, "table-scroll-regions", r.length >= 4 && bad.length === 0, `wrappers=${r.length} bad=${bad.length}`);
  });
  const active = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      return { id: a.id, tag: a.tagName, text: (a.textContent || "").trim().slice(0, 30), dialog: (a.closest("dialog") || {}).id || "" };
    });
  for (const [side, opener, first] of [
    ["left", "#gallery-sheet-left-open", "A"],
    ["right", "#gallery-sheet-right-open", "INPUT"],
  ]) {
    const id = "gallery-sheet-" + side;
    await t(`sheet-${side}`, async () => {
      await page.focus(opener);
      await page.keyboard.press("Enter");
      await page.waitForSelector(`#${id}[open]`);
      await settle();
      const a = await active();
      const geo = await page.$eval("#" + id, (d) => {
        const r = d.getBoundingClientRect();
        return { left: Math.round(r.left), right: Math.round(window.innerWidth - r.right), modal: d.matches(":modal") };
      });
      const expanded = await page.$eval(opener, (b) => b.getAttribute("aria-expanded"));
      let escaped = 0;
      for (let i = 0; i < 14; i++) {
        await page.keyboard.press(i % 3 === 2 ? "Shift+Tab" : "Tab");
        const x = await active();
        if (x.dialog !== id && x.tag !== "BODY") escaped++;
      }
      await page.keyboard.press("Escape");
      await settle();
      const closed = await page.$eval("#" + id, (d) => !d.open);
      const back = await active();
      const after = await page.$eval(opener, (b) => b.getAttribute("aria-expanded"));
      const edge = side === "left" ? geo.left === 0 : geo.right === 0;
      check(
        scheme,
        `sheet-${side}`,
        a.dialog === id && a.tag === first && geo.modal && edge && expanded === "true" && escaped === 0 && closed && "#" + back.id === opener && after === "false",
        `focus=${a.tag}${a.id ? "#" + a.id : ""} modal=${geo.modal} edge(${side})=${edge} expanded=${expanded}->${after} trap-escapes=${escaped} esc-closed=${closed} focus-return=#${back.id}`,
      );
    });
  }
  page.setDefaultTimeout(30000);
}

// Record whether each page load was a bfcache restore (pageshow.persisted),
// so the into/away/back checks can say which kind of Back they drove.
const PAGESHOW_PROBE = () => {
  window.__gfPageshow = [];
  window.addEventListener("pageshow", (e) => window.__gfPageshow.push(e.persisted ? "bfcache" : "load"));
};

async function waitPath(page, want) {
  await page.waitForFunction((p) => location.pathname === p, want, { timeout: 8000 });
  await page.waitForLoadState("load");
}

// Scroll-lock probes (t1486u). prepScroll makes the page taller than the
// viewport with a test-only spacer and scrolls it to y.
async function prepScroll(page, y) {
  await page.evaluate((y) => {
    const host = document.querySelector("main") || document.body;
    const pad = document.createElement("div");
    pad.id = "ui-a11y-scroll-spacer";
    pad.style.height = "3000px";
    host.appendChild(pad);
    window.scrollTo({ top: y, behavior: "instant" });
  }, y);
}

function scrollLockState(page) {
  return page.evaluate(() => {
    const h = document.documentElement;
    const b = document.body;
    const spacer = document.getElementById("ui-a11y-scroll-spacer");
    return {
      dialogOpen: !!document.querySelector("dialog[open]"),
      locked: h.hasAttribute("data-ui-scroll-locked"),
      scrollY: Math.round(window.scrollY),
      htmlOverflow: getComputedStyle(h).overflow,
      bodyPosition: getComputedStyle(b).position,
      bodyTop: Math.round(parseFloat(b.style.top || "0")),
      visualTop: spacer ? Math.round(spacer.getBoundingClientRect().top) : null,
      htmlStyle: h.getAttribute("style"),
      bodyStyle: b.getAttribute("style"),
    };
  });
}

function fmtLock(s) {
  return `locked=${s.locked} scrollY=${s.scrollY} html.overflow=${s.htmlOverflow} body.position=${s.bodyPosition} body.top=${s.bodyTop} spacer.top=${s.visualTop}`;
}

// A touch swipe up over the backdrop: CDP Input.synthesizeScrollGesture
// with a touch source drives a real touch scroll without touch emulation.
async function swipe(page, x) {
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send("Input.synthesizeScrollGesture", { x, y: 600, yDistance: -500, gestureSourceType: "touch", speed: 2000 });
  } finally {
    await cdp.detach();
  }
  await page.waitForTimeout(350);
  return scrollLockState(page);
}

// PR6 app checks: the mobile sidebar is a Sheet opened from the header
// button at a 375px viewport; the desktop aside is hidden there.
async function appSheetChecks(scheme, page) {
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  const before = page.viewportSize();
  page.setDefaultTimeout(8000);
  const settle = () => page.waitForTimeout(350);
  const active = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      return { id: a.id, tag: a.tagName, href: a.getAttribute("href") || "", dialog: (a.closest("dialog") || {}).id || "" };
    });
  const sheetState = () =>
    page.evaluate(() => {
      const d = document.getElementById("mobile-nav");
      const cur = Array.from(d.querySelectorAll('a[data-nav][aria-current="page"]'));
      return {
        path: location.pathname,
        open: d.open,
        expanded: document.getElementById("mobile-menu-btn").getAttribute("aria-expanded"),
        current: cur.length,
        currentHref: cur[0] ? cur[0].getAttribute("href") : "",
        shows: (window.__gfPageshow || []).join(","),
      };
    });
  // Open from #mobile-menu-btn with Enter, then trap, Escape, focus return.
  const roundTrip = async () => {
    await page.focus("#mobile-menu-btn");
    await page.keyboard.press("Enter");
    await page.waitForSelector("#mobile-nav[open]");
    await settle();
    const a = await active();
    const modal = await page.$eval("#mobile-nav", (d) => d.matches(":modal"));
    const expanded = await page.$eval("#mobile-menu-btn", (b) => b.getAttribute("aria-expanded"));
    let escaped = 0;
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press(i % 3 === 2 ? "Shift+Tab" : "Tab");
      const x = await active();
      if (x.dialog !== "mobile-nav" && x.tag !== "BODY") escaped++;
    }
    await page.keyboard.press("Escape");
    await settle();
    const closed = await page.$eval("#mobile-nav", (d) => !d.open);
    const back = await active();
    const after = await page.$eval("#mobile-menu-btn", (b) => b.getAttribute("aria-expanded"));
    const ok = a.dialog === "mobile-nav" && a.tag === "A" && modal && expanded === "true" && escaped === 0 && closed && back.id === "mobile-menu-btn" && after === "false";
    return { ok, detail: `open(focus=${a.tag} modal=${modal} expanded=${expanded}->${after} trap-escapes=${escaped} esc-closed=${closed} focus-return=#${back.id})` };
  };
  try {
    await page.setViewportSize({ width: 375, height: 800 });
    await t("app-mobile-sheet", async () => {
      await page.goto(BASE + "/dashboard", { waitUntil: "load" });
      const pre = await page.evaluate(() => ({
        aside: getComputedStyle(document.getElementById("sidebar")).display,
        overlay: !!document.getElementById("sidebar-overlay"),
        inline: Array.from(document.scripts).filter((s) => !s.src && !s.type.includes("json")).length,
        btn: document.getElementById("mobile-menu-btn").getBoundingClientRect().width > 0,
      }));
      await page.focus("#mobile-menu-btn");
      await page.keyboard.press("Enter");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      const a = await active();
      const geo = await page.$eval("#mobile-nav", (d) => {
        const r = d.getBoundingClientRect();
        return { left: Math.round(r.left), width: Math.round(r.width), modal: d.matches(":modal"), links: d.querySelectorAll("a[data-nav]").length, current: d.querySelectorAll('a[data-nav][aria-current="page"]').length };
      });
      const expanded = await page.$eval("#mobile-menu-btn", (b) => b.getAttribute("aria-expanded"));
      const shot = path.join(EVIDENCE, "ui-a11y", scheme, "mobile-nav-sheet.png");
      await page.screenshot({ path: shot });
      fs.chmodSync(shot, 0o644);
      let escaped = 0;
      for (let i = 0; i < 14; i++) {
        await page.keyboard.press(i % 3 === 2 ? "Shift+Tab" : "Tab");
        const x = await active();
        if (x.dialog !== "mobile-nav" && x.tag !== "BODY") escaped++;
      }
      await page.keyboard.press("Escape");
      await settle();
      const closed = await page.$eval("#mobile-nav", (d) => !d.open);
      const back = await active();
      const after = await page.$eval("#mobile-menu-btn", (b) => b.getAttribute("aria-expanded"));
      check(
        scheme,
        "app-mobile-sheet",
        pre.aside === "none" && !pre.overlay && pre.btn && a.dialog === "mobile-nav" && a.tag === "A" && geo.modal && geo.left === 0 && geo.current === 1 &&
          expanded === "true" && escaped === 0 && closed && back.id === "mobile-menu-btn" && after === "false",
        `375px aside=${pre.aside} overlay=${pre.overlay} inline-scripts=${pre.inline} focus=${a.tag}[${a.href}] modal=${geo.modal} left=${geo.left} width=${geo.width} links=${geo.links} current=${geo.current} expanded=${expanded}->${after} trap-escapes=${escaped} esc-closed=${closed} focus-return=#${back.id}`,
      );
    });
    // Into/away/back (Oct 9 bar): open the Sheet (into), follow a link
    // inside it with Enter (away), then Back, Forward, and Back again. The
    // Sheet must never come back open (bfcache included), aria-expanded must
    // read false, the current link must follow the route, and the Sheet must
    // still open with its trap, Escape, and focus return on each page.
    await t("app-mobile-sheet-link-away-back", async () => {
      await page.addInitScript(PAGESHOW_PROBE);
      await page.goto(BASE + "/dashboard", { waitUntil: "load" });
      const steps = [];
      let ok = true;
      const expect = (label, s, want) => {
        const good = s.path === want && !s.open && s.expanded === "false" && s.current === 1 && s.currentHref === want;
        if (!good) ok = false;
        steps.push(`${label}:${s.path} open=${s.open} expanded=${s.expanded} current=${s.current}@${s.currentHref}${good ? "" : "!"}`);
      };
      // into
      await page.focus("#mobile-menu-btn");
      await page.keyboard.press("Enter");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      // away: Enter on the Projects link inside the Sheet
      await page.focus('#mobile-nav a[data-nav][href="/projects"]');
      await Promise.all([waitPath(page, "/projects"), page.keyboard.press("Enter")]);
      await settle();
      expect("away", await sheetState(), "/projects");
      let rt = await roundTrip();
      if (!rt.ok) ok = false;
      steps.push(`away-reopen ${rt.detail}${rt.ok ? "" : "!"}`);
      // back to the page the Sheet was open on
      await page.goBack({ waitUntil: "load" });
      await waitPath(page, "/dashboard");
      await settle();
      const sb = await sheetState();
      expect("back", sb, "/dashboard");
      steps.push(`back-pageshow=${sb.shows}`);
      rt = await roundTrip();
      if (!rt.ok) ok = false;
      steps.push(`back-reopen ${rt.detail}${rt.ok ? "" : "!"}`);
      // forward, then back once more
      await page.goForward({ waitUntil: "load" });
      await waitPath(page, "/projects");
      await settle();
      expect("forward", await sheetState(), "/projects");
      await page.goBack({ waitUntil: "load" });
      await waitPath(page, "/dashboard");
      await settle();
      expect("back2", await sheetState(), "/dashboard");
      rt = await roundTrip();
      if (!rt.ok) ok = false;
      steps.push(`back2-reopen ${rt.detail}${rt.ok ? "" : "!"}`);
      check(scheme, "app-mobile-sheet-link-away-back", ok, "375px " + steps.join(" "));
    });
    // Open, Escape, then leave for another route and come back, both by
    // Back and by a fresh link from the Sheet, and open again.
    await t("app-mobile-sheet-escape-away-back", async () => {
      await page.goto(BASE + "/dashboard", { waitUntil: "load" });
      const steps = [];
      let ok = true;
      let rt = await roundTrip();
      if (!rt.ok) ok = false;
      steps.push(`dashboard ${rt.detail}${rt.ok ? "" : "!"}`);
      // away: Containers through the Sheet
      await page.click("#mobile-menu-btn");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      await Promise.all([waitPath(page, "/containers"), page.click('#mobile-nav a[data-nav][href="/containers"]')]);
      await settle();
      let s = await sheetState();
      let good = !s.open && s.expanded === "false" && s.currentHref === "/containers" && s.current === 1;
      if (!good) ok = false;
      steps.push(`away:${s.path} open=${s.open} current=${s.current}@${s.currentHref}${good ? "" : "!"}`);
      // back by history
      await page.goBack({ waitUntil: "load" });
      await waitPath(page, "/dashboard");
      await settle();
      s = await sheetState();
      good = !s.open && s.expanded === "false" && s.currentHref === "/dashboard" && s.current === 1;
      if (!good) ok = false;
      steps.push(`back:${s.path} open=${s.open} current=${s.current}@${s.currentHref} pageshow=${s.shows}${good ? "" : "!"}`);
      rt = await roundTrip();
      if (!rt.ok) ok = false;
      steps.push(`back-reopen ${rt.detail}${rt.ok ? "" : "!"}`);
      // back by link: Dashboard from the Sheet on another page
      await page.goto(BASE + "/services", { waitUntil: "load" });
      await page.click("#mobile-menu-btn");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      await Promise.all([waitPath(page, "/dashboard"), page.click('#mobile-nav a[data-nav][href="/dashboard"]')]);
      await settle();
      s = await sheetState();
      good = !s.open && s.expanded === "false" && s.currentHref === "/dashboard" && s.current === 1;
      if (!good) ok = false;
      steps.push(`link-back:${s.path} open=${s.open} current=${s.current}@${s.currentHref}${good ? "" : "!"}`);
      rt = await roundTrip();
      if (!rt.ok) ok = false;
      steps.push(`link-back-reopen ${rt.detail}${rt.ok ? "" : "!"}`);
      check(scheme, "app-mobile-sheet-escape-away-back", ok, "375px " + steps.join(" "));
    });
    // t1486u: the page behind the open Sheet must not scroll. A spacer
    // makes the dashboard taller than the viewport; scroll it to 300, open
    // the Sheet, wheel and swipe over the backdrop (right of the panel),
    // then close with Escape: the page must sit still while open and come
    // back at 300 with its inline styles restored.
    await t("app-mobile-sheet-scroll-lock", async () => {
      await page.goto(BASE + "/dashboard", { waitUntil: "load" });
      await prepScroll(page, 300);
      await page.click("#mobile-menu-btn");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      const open = await scrollLockState(page);
      const panel = await page.$eval("#mobile-nav", (d) => Math.round(d.getBoundingClientRect().right));
      const x = Math.min(370, panel + 20);
      await page.mouse.move(x, 600);
      await page.mouse.wheel(0, 900);
      await settle();
      const wheeled = await scrollLockState(page);
      const swiped = await swipe(page, x);
      await page.keyboard.press("Escape");
      await settle();
      const closed = await scrollLockState(page);
      const focus = await page.evaluate(() => document.activeElement.id);
      const pinned = (s) => s.htmlOverflow === "hidden" && s.bodyPosition === "fixed" && s.bodyTop === -300;
      const ok =
        open.dialogOpen && pinned(open) && pinned(wheeled) && pinned(swiped) && wheeled.visualTop === open.visualTop && swiped.visualTop === open.visualTop &&
        !closed.dialogOpen && closed.scrollY === 300 && closed.htmlStyle === null && closed.bodyStyle === null && !closed.locked && focus === "mobile-menu-btn";
      check(
        scheme,
        "app-mobile-sheet-scroll-lock",
        ok,
        `375px backdrop-x=${x} open(${fmtLock(open)}) wheel(${fmtLock(wheeled)}) swipe(${fmtLock(swiped)}) esc(${fmtLock(closed)}) focus-return=#${focus}`,
      );
    });
    // t1489u: the Sheet title ("Menu") and its X share one center line.
    await t("app-mobile-sheet-header-align", async () => {
      await page.goto(BASE + "/dashboard", { waitUntil: "load" });
      await page.click("#mobile-menu-btn");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      const g = await page.$eval("#mobile-nav", (d) => {
        const mid = (r) => r.top + r.height / 2;
        const h = d.querySelector("h2").getBoundingClientRect();
        const x = d.querySelector("[data-ui-dialog-close]").getBoundingClientRect();
        return { title: Math.round(h.height), button: Math.round(x.height), dy: Math.round((mid(h) - mid(x)) * 10) / 10 };
      });
      await page.keyboard.press("Escape");
      await settle();
      check(scheme, "app-mobile-sheet-header-align", Math.abs(g.dy) <= 1, `375px title-h=${g.title} close-h=${g.button} center-dy=${g.dy}px`);
    });
    await t("app-mobile-sheet-closes-at-md", async () => {
      await page.goto(BASE + "/dashboard", { waitUntil: "load" });
      await prepScroll(page, 200);
      await page.click("#mobile-menu-btn");
      await page.waitForSelector("#mobile-nav[open]");
      await settle();
      const locked = await scrollLockState(page);
      await page.setViewportSize({ width: 1024, height: 800 });
      await settle();
      const s = await page.evaluate(() => ({ open: document.getElementById("mobile-nav").open, aside: getComputedStyle(document.getElementById("sidebar")).display }));
      const u = await scrollLockState(page);
      const unlocked = !u.locked && u.htmlStyle === null && u.bodyStyle === null && u.htmlOverflow !== "hidden" && u.bodyPosition !== "fixed" && u.scrollY === 200;
      check(
        scheme,
        "app-mobile-sheet-closes-at-md",
        !s.open && s.aside === "flex" && locked.locked && unlocked,
        `open=${s.open} aside=${s.aside} lock-while-open=${locked.locked} after-widen(${fmtLock(u)})`,
      );
    });
  } finally {
    if (before) await page.setViewportSize(before);
    page.setDefaultTimeout(30000);
  }
}

// One aria-current="page" link in the project NavTabs, on the given path.
function navTabsState(page) {
  return page.evaluate(() => {
    const nav = document.querySelector("nav[data-ui-navtabs]");
    if (!nav) return null;
    const links = Array.from(nav.querySelectorAll("a[href]"));
    const cur = links.filter((a) => a.getAttribute("aria-current") === "page");
    return {
      path: location.pathname,
      label: nav.getAttribute("aria-label"),
      n: cur.length,
      href: cur[0] ? cur[0].getAttribute("href").split("?")[0] : "",
      hrefs: links.map((a) => a.getAttribute("href").split("?")[0]),
      roles: nav.querySelectorAll("[role=tab],[role=tablist]").length,
      tabbable: links.filter((a) => a.tabIndex === 0).length,
      shows: (window.__gfPageshow || []).join(","),
    };
  });
}

// PR6: project detail tabs are server-routed NavTabs with one
// aria-current="page" per route.
async function appNavTabsChecks(scheme, page, projectTabs) {
  if (!projectTabs.length) {
    check(scheme, "app-navtabs", false, "no project to drive");
    return;
  }
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  const short = (p) => p.replace(/^\/projects\/[^/]+/, "") || "/";
  await t("app-navtabs", async () => {
    const seen = [];
    let ok = true;
    for (const href of projectTabs) {
      await page.goto(BASE + href, { waitUntil: "load" });
      const r = await navTabsState(page);
      const path0 = href.split("?")[0];
      const good = r && r.n === 1 && r.roles === 0 && r.href === path0;
      if (!good) ok = false;
      seen.push(`${short(path0)}:${r ? r.n + "@" + short(r.href) : "none"}`);
    }
    check(scheme, "app-navtabs", ok, seen.join(" "));
  });
  // Into/away/back (Oct 9 bar). From Overview, move into each other tab
  // through the NavTabs links (click, then keyboard Enter on a focused
  // link), leave the project for the sidebar's Projects and Dashboard, and
  // come back by Back, by Forward, and by the project's link in the list.
  // Every stop needs exactly one aria-current on the route's own link, all
  // links still tabbable, and no tab roles.
  const overview = projectTabs[0].split("?")[0];
  const steps = [];
  let ok = true;
  const expect = async (label, want) => {
    const r = await navTabsState(page);
    const good = r && r.path === want && r.n === 1 && r.href === want && r.roles === 0 && r.tabbable === r.hrefs.length && r.hrefs.length === projectTabs.length;
    if (!good) ok = false;
    steps.push(`${label}:${r ? short(r.path) + " " + r.n + "@" + short(r.href) + " tabbable=" + r.tabbable + "/" + r.hrefs.length : "none"}${good ? "" : "!"}`);
    return r;
  };
  const tabLink = (p) => `nav[data-ui-navtabs] :is(a[href="${p}"], a[href^="${p}?"])`;
  await t("app-navtabs-into-away-back", async () => {
    page.setDefaultTimeout(8000);
    await page.addInitScript(PAGESHOW_PROBE);
    await page.goto(BASE + overview, { waitUntil: "load" });
    await expect("start", overview);
    // into each tab by click, then back to Overview by keyboard
    for (const href of projectTabs.slice(1)) {
      const p = href.split("?")[0];
      await Promise.all([waitPath(page, p), page.click(tabLink(p))]);
      await expect("click", p);
    }
    await page.focus(tabLink(overview));
    await Promise.all([waitPath(page, overview), page.keyboard.press("Enter")]);
    const k = await expect("enter", overview);
    // keyboard: Tab from the current tab link reaches the next tab link
    await page.focus(`nav[data-ui-navtabs] a[aria-current="page"]`);
    await page.keyboard.press("Tab");
    const next = await page.evaluate(() => (document.activeElement.closest("nav[data-ui-navtabs]") ? document.activeElement.getAttribute("href").split("?")[0] : "out"));
    const wantNext = k ? k.hrefs[1] : "";
    if (next !== wantNext) ok = false;
    steps.push(`tab-key->${short(next)}${next === wantNext ? "" : "!"}`);
    // last tab, then away through the sidebar
    const last = projectTabs[projectTabs.length - 1].split("?")[0];
    await Promise.all([waitPath(page, last), page.click(tabLink(last))]);
    await expect("into-last", last);
    await Promise.all([waitPath(page, "/projects"), page.click('#sidebar a[data-nav][href="/projects"]')]);
    steps.push("away:/projects");
    // back by history lands on the last tab, not a stale Overview
    await page.goBack({ waitUntil: "load" });
    await waitPath(page, last);
    const b = await expect("back", last);
    if (b) steps.push(`back-pageshow=${b.shows}`);
    await page.goForward({ waitUntil: "load" });
    await waitPath(page, "/projects");
    steps.push("forward:/projects");
    // back by link: the project's entry in the list opens Overview
    await Promise.all([waitPath(page, overview), page.locator(`main :is(a[href="${overview}"], a[href^="${overview}?"])`).first().click()]);
    await expect("link-back", overview);
    // switch tabs, leave for Dashboard, Back twice through the history
    const mid = projectTabs[1].split("?")[0];
    await Promise.all([waitPath(page, mid), page.click(tabLink(mid))]);
    await expect("into-mid", mid);
    await Promise.all([waitPath(page, "/dashboard"), page.click('#sidebar a[data-nav][href="/dashboard"]')]);
    steps.push("away:/dashboard");
    await page.goBack({ waitUntil: "load" });
    await waitPath(page, mid);
    await expect("back", mid);
    await page.goBack({ waitUntil: "load" });
    await waitPath(page, overview);
    await expect("back2", overview);
    check(scheme, "app-navtabs-into-away-back", ok, steps.join(" "));
  });
  page.setDefaultTimeout(30000);
}

// t1681u: detail and post-PR7 pages. Ids come from env, else from links on
// the list pages (service instances, containers, the project's
// Deployments tab). A missing id is a skip line, not a fail, so a drive
// without seed rows still runs; ui-a11y.md has the host-mode seed SQL.
const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";

async function firstHref(page, url, re) {
  await page.goto(BASE + url, { waitUntil: "load" });
  await page.waitForTimeout(600);
  return page.evaluate((src) => {
    const rx = new RegExp(src);
    const a = Array.from(document.querySelectorAll("a[href]")).find((x) => rx.test(x.getAttribute("href")));
    return a ? a.getAttribute("href") : null;
  }, re.source);
}

async function detailIDs(page, projectHref) {
  const env = process.env;
  let service = env.GOFORGE_UI_SERVICE_ID || "";
  if (!service) {
    const h = await firstHref(page, "/services", new RegExp(`^/services/${UUID}$`));
    service = h ? h.split("/")[2] : "";
  }
  let container = env.GOFORGE_UI_CONTAINER_ID || "";
  if (!container) {
    const h = await firstHref(page, "/containers", /^\/containers\/[0-9a-f]{12,64}$/);
    container = h ? h.split("/")[2] : "";
  }
  let deployment = env.GOFORGE_UI_DEPLOYMENT_PATH || "";
  if (!deployment && projectHref) {
    deployment = (await firstHref(page, projectHref + "/deployments", new RegExp(`^/projects/${UUID}/deployments/${UUID}$`))) || "";
  }
  return { service, container, deployment };
}

function detailRouteList(ids) {
  const routes = [];
  if (ids.service) routes.push(`/services/${ids.service}`, `/services/${ids.service}/edit`);
  if (ids.container) routes.push(`/containers/${ids.container}`);
  if (ids.deployment) routes.push(ids.deployment);
  return routes;
}

async function axeIn(page, sel) {
  await page.evaluate(AXE_SOURCE);
  const r = await page.evaluate(
    ([tags, sel]) => window.axe.run({ include: [[sel]] }, { runOnly: { type: "tag", values: tags }, resultTypes: ["violations"] }),
    [AXE_TAGS, sel],
  );
  return r.violations.filter((v) => GATE_IMPACTS.has(v.impact)).map((v) => `${v.id}(${v.impact},${v.nodes.length})`);
}

// Service detail (#107, #104/#105) and the page hooks app.js took over from
// inline scripts (t1681u gap 3). Never types the real service name, so the
// typed delete only ever sends a refused request.
async function appDetailChecks(scheme, page, ids) {
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  page.setDefaultTimeout(8000);
  const settle = () => page.waitForTimeout(350);
  const active = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      return { id: a ? a.id : "", tag: a ? a.tagName : "", dialog: a && a.closest("dialog") ? a.closest("dialog").id : "", testid: a ? a.getAttribute("data-testid") || "" : "" };
    });

  await t("app-git-source-toggle", async () => {
    await page.goto(BASE + "/projects/new", { waitUntil: "load" });
    const s = await page.evaluate(() => {
      const m = document.getElementById("project-repo-manual");
      const sel = document.getElementById("project-repo-select");
      const src = document.getElementById("project-git-source");
      return {
        hook: src ? src.hasAttribute("data-git-source-toggle") : false,
        inline: src ? Array.from(src.attributes).filter((a) => /^on/.test(a.name)).length : -1,
        manualName: m ? m.getAttribute("name") : "",
        manualRequired: m ? m.required : false,
        selectName: sel ? sel.getAttribute("name") : "",
        selectShown: sel ? getComputedStyle(document.getElementById("repo-select-container")).display !== "none" : true,
      };
    });
    check(
      scheme,
      "app-git-source-toggle",
      s.hook && s.inline === 0 && s.manualName === "repository_url" && s.manualRequired && s.selectName === "repository_url_select_disabled" && !s.selectShown,
      `hook=${s.hook} on*=${s.inline} manual name=${s.manualName} required=${s.manualRequired} select name=${s.selectName} selectShown=${s.selectShown}`,
    );
  });

  if (ids.container) {
    await t("app-container-status-swap", async () => {
      await page.goto(BASE + `/containers/${ids.container}`, { waitUntil: "load" });
      const read = () => page.$eval("#container-status", (el) => el.textContent.replace(/\s+/g, " ").trim().toLowerCase());
      const before = await read();
      // The same event the stop/restart HX-Trigger header raises; no Docker call.
      await page.evaluate(() => document.body.dispatchEvent(new CustomEvent("containerStopped", { bubbles: true })));
      const stopped = await read();
      await page.evaluate(() => document.body.dispatchEvent(new CustomEvent("containerRestarted", { bubbles: true })));
      const running = await read();
      const scripts = await page.$$eval("script:not([src])", (s) => s.length);
      check(scheme, "app-container-status-swap", /exited/.test(stopped) && /running/.test(running) && scripts === 0, `before=${before} stopped=${stopped} restarted=${running} inline-scripts=${scripts}`);
    });
  } else {
    lines.push(`check ${scheme} app-container-status-swap skip reason=no-container-id`);
  }

  if (!ids.service) {
    lines.push(`check ${scheme} app-typed-delete skip reason=no-service-id`);
    page.setDefaultTimeout(30000);
    return;
  }
  const svc = `/services/${ids.service}`;

  await t("app-secret-show-hide", async () => {
    await page.goto(BASE + svc, { waitUntil: "load" });
    const row = "[data-secret]";
    if (!(await page.$(row))) {
      lines.push(`check ${scheme} app-secret-show-hide skip reason=no-masked-row`);
      return;
    }
    const mask = await page.$eval(row, (el) => el.getAttribute("data-secret-mask"));
    await page.focus(`${row} [data-secret-action="toggle"]`);
    await page.keyboard.press("Enter");
    await page.waitForSelector(`${row} [data-secret-value][data-revealed="true"]`);
    const shown = await page.$eval(row, (el) => ({ differs: el.querySelector("[data-secret-value]").textContent !== el.getAttribute("data-secret-mask"), btn: el.querySelector('[data-secret-action="toggle"]').textContent.trim() }));
    await page.keyboard.press("Enter");
    await settle();
    const hidden = await page.$eval(row, (el) => ({ masked: el.querySelector("[data-secret-value]").textContent === el.getAttribute("data-secret-mask"), btn: el.querySelector('[data-secret-action="toggle"]').textContent.trim() }));
    const scripts = await page.$$eval("script:not([src])", (s) => s.length);
    // The value itself is never written to evidence.
    check(scheme, "app-secret-show-hide", Boolean(mask) && shown.differs && shown.btn === "Hide" && hidden.masked && hidden.btn === "Show" && scripts === 0, `revealed=${shown.differs} btn=${shown.btn} re-masked=${hidden.masked} btn=${hidden.btn} inline-scripts=${scripts}`);
  });

  await t("app-typed-delete", async () => {
    await page.goto(BASE + svc, { waitUntil: "load" });
    let deletes = 0;
    const onReq = (r) => {
      if (r.method() === "DELETE") deletes++;
    };
    page.on("request", onReq);
    const opener = '[data-ui-dialog-open="delete-service-dialog"]';
    const dlg = "#delete-service-dialog";
    await page.focus(opener);
    await page.keyboard.press("Enter");
    await page.waitForSelector(dlg + "[open]");
    await settle();
    const a = await active();
    let escaped = 0;
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press(i % 3 === 2 ? "Shift+Tab" : "Tab");
      const x = await active();
      if (x.dialog !== "delete-service-dialog" && x.tag !== "BODY") escaped++;
    }
    const axeBad = await axeIn(page, dlg);
    // Empty name: the required input blocks the submit, so no DELETE.
    await page.click(`${dlg} [data-testid="delete-service-dialog-submit"]`);
    await settle();
    const emptyDeletes = deletes;
    // Wrong name: the server refuses (400) and the dialog shows its error.
    await page.fill(`${dlg} [data-testid="delete-service-dialog-input"]`, "ui-a11y-wrong-name");
    await page.click(`${dlg} [data-testid="delete-service-dialog-submit"]`);
    await page.waitForSelector(`${dlg} [data-ui-dialog-error]:not(.hidden)`);
    const err = await page.$eval(`${dlg} [data-ui-dialog-error]`, (el) => el.textContent.trim().length > 0);
    const stillOpen = await page.$eval(dlg, (d) => d.open);
    await page.keyboard.press("Escape");
    await settle();
    const closed = await page.$eval(dlg, (d) => !d.open);
    const back = await active();
    const stays = new URL(page.url()).pathname === svc;
    page.off("request", onReq);
    check(
      scheme,
      "app-typed-delete",
      a.dialog === "delete-service-dialog" && escaped === 0 && axeBad.length === 0 && emptyDeletes === 0 && deletes === 1 && err && stillOpen && closed && back.dialog === "" && stays,
      `focus in=${a.dialog} trap-escaped=${escaped} axe serious/critical=${axeBad.length}${axeBad.length ? " " + axeBad.join(",") : ""} empty-name DELETEs=${emptyDeletes} wrong-name DELETEs=${deletes} error-shown=${err} open-after-error=${stillOpen} esc-closed=${closed} focus-return=${back.tag}${back.testid ? "[" + back.testid + "]" : ""} still-on-page=${stays}`,
    );
  });

  await t("app-protected-switch", async () => {
    await page.goto(BASE + svc, { waitUntil: "load" });
    const sw = "#service-protected-switch";
    if (await page.$eval(sw, (el) => el.disabled)) {
      lines.push(`check ${scheme} app-protected-switch skip reason=env-locked`);
      return;
    }
    const before = await page.$eval(sw, (el) => el.checked);
    const toggle = async () => {
      await page.focus(sw);
      await Promise.all([page.waitForResponse((r) => r.url().endsWith(`${svc}/protected`) && r.request().method() === "POST"), page.keyboard.press("Space")]);
      await settle();
      return page.$eval(sw, (el) => ({ checked: el.checked, role: el.getAttribute("role"), focused: document.activeElement === el }));
    };
    const one = await toggle();
    const two = await toggle();
    const axeBad = await axeIn(page, "#service-protected");
    check(
      scheme,
      "app-protected-switch",
      one.checked === !before && two.checked === before && one.role === "switch" && axeBad.length === 0,
      `start=${before} after-space=${one.checked} after-second-space=${two.checked} role=${one.role} focus-kept=${one.focused}/${two.focused} axe serious/critical=${axeBad.length}`,
    );
  });

  await t("app-restore-points", async () => {
    await page.goto(BASE + svc, { waitUntil: "load" });
    const card = await page.$("#service-restore-points");
    if (!card) {
      lines.push(`check ${scheme} app-restore-points skip reason=card-not-rendered (needs admin and a backed-up template)`);
      return;
    }
    await page.waitForFunction(() => !/Loading restore points/.test(document.getElementById("service-restore-points").textContent), null, { timeout: 8000 });
    const state = await page.evaluate(() => {
      const el = document.getElementById("service-restore-points");
      return el.querySelector('[data-testid="service-restore-point-table"]') ? "table" : el.querySelector('[data-testid="service-restore-points-empty"]') ? "empty" : el.querySelector('[data-testid="service-restore-points-error"]') ? "error" : "other";
    });
    const axeBad = await axeIn(page, "#service-restore-points");
    check(scheme, "app-restore-points", axeBad.length === 0 && state !== "other", `state=${state} axe serious/critical=${axeBad.length}${axeBad.length ? " " + axeBad.join(",") : ""}`);
  });
  page.setDefaultTimeout(30000);
}

// ROADMAP item 5 (t1681u gap 2): below md every visible text button and
// field is at least 44px tall; from md up ButtonSM keeps its compact size.
// ButtonInline (prose links) and checkboxes are out of scope.
async function touchTargetChecks(scheme, page) {
  const t = (name, fn) => fn().catch((e) => check(scheme, name, false, "threw " + String(e.message || e).split("\n")[0]));
  const before = page.viewportSize();
  const measure = () =>
    page.evaluate(() => {
      const sel = ".btn, [class*='btn-'], .input, .select, input.input, select.select, textarea.input";
      const out = { n: 0, small: [], sm: [] };
      for (const el of document.querySelectorAll(sel)) {
        const cls = el.className && el.className.baseVal === undefined ? el.className : "";
        if (!/(^|\s)(btn-(primary|secondary|danger|warning|ghost|ghost-primary|ghost-warning|ghost-danger|link)|input|select)(\s|$)/.test(cls)) continue;
        if (/(^|\s)btn-inline(\s|$)/.test(cls)) continue;
        if (el.closest("dialog:not([open]), [popover]:not(:popover-open), template")) continue;
        const r = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        if (r.width === 0 || r.height === 0 || cs.visibility === "hidden" || cs.display === "none") continue;
        out.n++;
        const h = Math.round(r.height * 10) / 10;
        if (h < 44) out.small.push(`${el.tagName.toLowerCase()}${el.id ? "#" + el.id : ""}=${h}`);
        if (/(^|\s)btn-sm(\s|$)/.test(cls)) out.sm.push(h);
      }
      return out;
    });
  for (const route of ["/settings/profile", "/projects/new", "/dashboard"]) {
    await t(`touch-targets ${route}`, async () => {
      await page.setViewportSize({ width: 375, height: 812 });
      await page.goto(BASE + route, { waitUntil: "load" });
      await page.waitForTimeout(500);
      const mobile = await measure();
      await page.setViewportSize(before);
      await page.goto(BASE + route, { waitUntil: "load" });
      await page.waitForTimeout(500);
      const desk = await measure();
      const deskSmMax = desk.sm.length ? Math.max(...desk.sm) : 0;
      check(
        scheme,
        `touch-targets ${route}`,
        mobile.n > 0 && mobile.small.length === 0 && deskSmMax < 44,
        `375px controls=${mobile.n} under-44=${mobile.small.length}${mobile.small.length ? " [" + mobile.small.slice(0, 8).join(",") + "]" : ""} desktop btn-sm=${desk.sm.length} max=${deskSmMax}`,
      );
    });
  }
  await page.setViewportSize(before);
}

// Sign in by posting the real login form from the browser context, so the
// session cookie lands in the context's jar. drive-register stores the
// password with a trailing newline (curl --data-urlencode @file sends it
// as is), and a browser strips line breaks from password inputs, so typing
// it cannot work. The CSRF token is read from the form and never written out.
// t1683u: the dashboard's Recent agent actions card. Empty, it shows the
// message with "View all activity" under it and no header action or table;
// with rows, "View all" sits in the header. axe inside the card is clean.
async function agentActionsCardCheck(scheme, page) {
  try {
    await page.goto(BASE + "/dashboard", { waitUntil: "load" });
    await page.waitForSelector("#recent-agent-actions", { timeout: 8000 });
    const s = await page.$eval("#recent-agent-actions", (c) => {
      const empty = c.querySelector('[data-testid="recent-agent-actions-empty"]');
      const header = c.firstElementChild;
      const headerLink = header ? header.querySelector('a[href^="/settings/activity"]') : null;
      const emptyLink = empty ? empty.querySelector('a[href="/settings/activity"]') : null;
      const below = empty && emptyLink ? emptyLink.getBoundingClientRect().top >= empty.querySelector("h3").getBoundingClientRect().bottom : false;
      return { empty: Boolean(empty), table: Boolean(c.querySelector("table")), headerLink: Boolean(headerLink), emptyLink: Boolean(emptyLink), below };
    });
    const axeBad = await axeIn(page, "#recent-agent-actions");
    const ok = (s.empty ? !s.table && !s.headerLink && s.emptyLink && s.below : s.table && s.headerLink) && axeBad.length === 0;
    check(scheme, "app-agent-actions-card", ok, `state=${s.empty ? "empty" : "rows"} table=${s.table} header-view-all=${s.headerLink} empty-view-all=${s.emptyLink} link-below-message=${s.below} axe serious/critical=${axeBad.length}`);
  } catch (e) {
    check(scheme, "app-agent-actions-card", false, "threw " + String(e.message || e).split("\n")[0]);
  }
}

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
      await overlayChecks(scheme, page);
      await tabsTableSheetChecks(scheme, page);
      if (!custom) {
        if (canLogin && (await login(page))) {
          lines.push(`login ${scheme} pass`);
          await appDialogChecks(scheme, page);
          const projectTabs = await projectRoutes(page);
          await appEnvMenuChecks(scheme, page, projectTabs[0]);
          await appNavTabsChecks(scheme, page, projectTabs);
          await appSheetChecks(scheme, page);
          const ids = await detailIDs(page, projectTabs[0]);
          lines.push(`detail-ids ${scheme} service=${ids.service ? "yes" : "none"} container=${ids.container ? "yes" : "none"} deployment=${ids.deployment ? "yes" : "none"}`);
          await appDetailChecks(scheme, page, ids);
          await touchTargetChecks(scheme, page);
          await agentActionsCardCheck(scheme, page);
          const routes = AUTH_ROUTES.concat(projectTabs, detailRouteList(ids));
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
