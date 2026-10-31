# UI kit foundation and accessibility

The UI reads its colors, radius, and focus ring from CSS variable tokens. Light and dark values follow `prefers-color-scheme`. A keyboard user sees a focus ring on buttons, links, and controls, but a mouse click does not draw one. With reduced motion requested, transitions and animations stop, except the loading spinner. `static/js/ui.js` initializes `data-ui` components once per element, including inside content that htmx swaps in. With `GOFORGE_DEV_MODE=true`, `/dev/ui` shows a gallery of the tokens, the kit components, and the legacy classes (`.btn*`, `.input`, `.label`, `.card*`). Without dev mode it returns `404`.

## Sub-features

- `token-scheme`: `--gf-background` on `:root` is `249 250 251` in light and `3 7 18` in dark.
- `focus-ring-mouse-hidden`: clicking `.btn-primary` on `/dev/ui` adds no box-shadow ring.
- `focus-ring-keyboard`: Tab focus on the same button adds the primary ring.
- `motion-default`: `.btn` has a `0.2s` transition.
- `motion-reduced`: with `reducedMotion: reduce`, the transition is `0.00001s` (`0.01ms`).
- `ui-js-delegation`: a probe `data-ui` component initializes once, and a delegated click is handled, both before and after a simulated htmx swap (`inits=2 clicks=2 first-pass=1`).
- Dialog and confirm (PR4), on the gallery's "Dialog and confirm" section. Requests go only to the dev route `/dev/ui/dialog` (`?ok=1` is `204`, `?fail=1` is `422`):
  - `dialog-open-focus`: Enter on the opener appends a native `<dialog>` to `<body>`, opens it modal, labels it by its `<h2>`, and puts focus inside.
  - `dialog-focus-trap`: 14 Tab and Shift+Tab presses never land outside the dialog.
  - `dialog-escape-focus-return`: Escape removes the ephemeral dialog and focus returns to the opener.
  - `dialog-stack`: a second dialog opened from the first gets its own id and focus, and Escape closes only the top one.
  - `dialog-backdrop`: a backdrop click closes a dialog, except a non-dismissible one, which Escape still closes.
  - `dialog-error`: a failed request writes its text into that dialog's own `role="alert"` node. There is no `#modal-error` and no toast.
  - `dialog-save-closes`: a successful submit closes the dialog and focus returns to the opener.
  - `dialog-static`: a `data-ui-dialog="static"` dialog is kept in the page after Escape.
  - `confirm-cancel`, `confirm-accept-keyboard`, `confirm-danger`: `hx-confirm` opens the kit `role="alertdialog"` with the question and the accept label. Escape and Cancel send nothing. Tab to Confirm and Enter sends exactly one request. The danger tone uses `btn-danger`.
  - `confirm-plain-form`: a plain form submit with `hx-confirm` is gated the same way and navigates only after Confirm.
  - `dialog-axe`: axe inside the open dialog and the open confirm finds nothing serious or critical other than the palette's known `color-contrast`.
  - `confirm-no-native-prompt`, `app-no-native-prompt`: no `window.confirm` prompt appears.
  - With a password: `app-token-dialog` drives Generate Token, submit, the swapped-in non-dismissible token dialog (a backdrop click keeps it), Escape, and focus return. `app-revoke-confirm-cancel` checks that Revoke asks through the kit confirm and that Cancel sends no `DELETE`.
- Overlays (PR5), on the gallery's "Dropdown, Popover, Tooltip" section. They use the native `popover` attribute and `popovertarget`, so they open without JS. `ui.js` adds placement, the menu keys, focus return, and `aria-expanded`:
  - `dropdown-keyboard`: Enter on the trigger opens the `role="menu"` with focus on the first item and `aria-expanded="true"`. ArrowDown and ArrowUp wrap, Home and End jump, and typeahead finds the next match. Escape closes the menu, sets `aria-expanded="false"`, and returns focus to the trigger.
  - `dropdown-open-keys`: ArrowUp on the trigger opens on the last item and ArrowDown on the first. Space opens it. Tab closes it and moves on from the trigger.
  - `dropdown-placement`: the menu opens 6 px below the trigger, flips above it near the bottom of the viewport, and `data-ui-align="end"` lines up the right edges.
  - `dropdown-light-dismiss`: an outside click and a second trigger click both close the menu.
  - `dropdown-item-request`, `dropdown-dialog-return`, `dropdown-confirm`: an htmx item sends one request and closes the menu. An item that opens a dialog hands focus back to the menu trigger when the dialog closes. A `hx-confirm` item asks through the kit confirm, and Escape there sends nothing.
  - `popover`: Enter opens the `role="dialog"` panel with focus inside. Escape, an outside click, and the close button each close it, and focus returns to the trigger.
  - `tooltip-focus`, `tooltip-hover`, `tooltip-icon-name`: a tooltip shows about 300 ms after focus or hover and is linked through `aria-describedby`. Escape hides it without moving focus. The pointer can move onto the tooltip without hiding it (WCAG 1.4.13). An icon button keeps its own `aria-label`, so the tooltip is never its only name.
  - `overlays-history`: `htmx:beforeHistorySave` closes every open overlay.
  - `overlays-axe`, `overlays-no-native-prompt`: inside the open overlays, axe finds nothing other than the palette's `color-contrast`, and no `window.confirm` appears.
  - With a password: `app-env-menu`, `app-env-new-dialog`, `app-env-link` drive the project's EnvironmentSwitcher. It has no `#menu-button` and no hardcoded `aria-expanded="true"`. The keys work as above, "New Environment" opens the kit dialog and focus returns to the trigger, and Enter on an environment navigates to `?env=…`.
- Tabs, Table, and Sheet (PR6), on the gallery's NavTabs, Tabs, Table, and Sheet sections:
  - `tabs-keyboard`: on the `role="tablist"`, ArrowRight and ArrowLeft wrap, and Home and End jump. Activation is automatic, so each key moves focus, `aria-selected`, the single `tabindex="0"` (roving tabindex), and the one visible `role="tabpanel"` together. Tab then leaves the tablist for the active panel.
  - `tabs-click`: a click selects the tab and shows its panel.
  - `table-scroll-regions`: every `data-ui-table` wrapper is a `role="region"` with `tabindex="0"`, named by the table's `<caption>` (visible or `sr-only`).
  - `sheet-left`, `sheet-right`: Enter on the opener opens the static `<dialog data-ui-sheet>` modal and flush to its edge, sets `aria-expanded="true"`, and focuses the first link or field (never the close button). 14 Tab and Shift+Tab presses stay inside. Escape closes it, focus returns to the opener, and `aria-expanded` goes back to `false`.
  - With a password: `app-navtabs` loads each project tab and expects one `aria-current="page"` link in the `Project sections` NavTabs, on that route, and no `role="tab"`. `app-mobile-sheet` sets a 375 px viewport on `/dashboard`: the desktop `#sidebar` aside is `display: none`, there is no `#sidebar-overlay`, and Enter on `#mobile-menu-btn` opens the `#mobile-nav` Sheet with the trap, Escape, and focus return as above, plus one current nav link. `app-mobile-sheet-closes-at-md` reopens it and widens the viewport to 1024 px: the Sheet closes (`data-ui-sheet-close-at`), the aside shows, and the scroll lock it held while open is gone (no inline `style` on `<html>` or `<body>`, page back at its offset). `app-mobile-sheet-scroll-lock` adds a test-only 3000 px spacer, scrolls to 300, opens the Sheet, and wheels and touch-swipes (CDP `Input.synthesizeScrollGesture`) over the backdrop: `<html>` is `overflow: hidden`, `<body>` is `position: fixed` at `top: -300px`, and the content does not move. Escape restores `scrollY` 300 and the inline styles, and focus returns to `#mobile-menu-btn`. `app-mobile-sheet-header-align` expects the `Menu` title and the close X to share a center line within 1 px. The drive restores the viewport afterwards and saves `ui-a11y/<scheme>/mobile-nav-sheet.png`.
  - Into, away, and back (the Oct 9 bar: every entry point, then navigating into the screen, away from it, and back, not only a fresh load). A `pageshow` probe records whether each Back was a bfcache restore or a full load, and the verdict prints it as `pageshow=`:
    - `app-navtabs-into-away-back`: from the project's Overview, a click on each other NavTabs link moves into that tab, and Enter on the focused Overview link moves back. Tab from the current link reaches the next one. The drive then opens the last tab, leaves through the sidebar's Projects link, and comes back with Back (the last tab, not Overview). It goes Forward, then follows the project's link in the list back to Overview. Last, it moves into Deployments, leaves for Dashboard, and goes Back twice (Deployments, then Overview). Every stop must be on the expected route with exactly one `aria-current="page"` on that route's own link. All four links must stay tabbable, and there must be no tab roles.
    - `app-mobile-sheet-link-away-back` (375 px): open `#mobile-nav` with Enter on `#mobile-menu-btn` (into), then press Enter on its Projects link (away). On `/projects` the Sheet is closed, `aria-expanded` is `false`, and the current link is Projects. Then go Back to `/dashboard`, Forward, and Back again. At each stop the Sheet is closed, never restored open, and the current link follows the route. Reopening it on `/projects` and after each Back keeps the trap, Escape, and focus return.
    - `app-mobile-sheet-escape-away-back` (375 px): open the Sheet and close it with Escape on `/dashboard`. Leave for `/containers` through the Sheet, come back with Back, and reopen it. Then go from `/services` back to Dashboard through the Sheet's Dashboard link and reopen it once more. Each time the Sheet is closed on arrival, has one current link, and passes the trap, Escape, and focus-return checks.
- `dev-ui-gated`: `/dev/ui` returns `200` with dev mode and `404` without it. The unit tests are `TestRouter_DevUIGalleryOnlyInDevMode` and `TestRouter_DevUIDialogDemoOnlyInDevMode`.
- `axe`: axe-core scans each page for WCAG 2.0/2.1 A and AA and 2.2 AA (`wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`, `wcag22aa`), in light and dark. The public pages are `/login`, `/auth/register`, `/dev/ui`, and a missing page. With a password, it also scans `/dashboard`, `/projects`, `/projects/new`, `/services`, `/containers`, `/settings/profile`, `/settings/git`, and the first project's Overview, Deployments, Settings, and Services tabs. `axe-gate=pass` means zero serious or critical findings.

## How to get to it (user POV)

1. Start GoForge with `GOFORGE_DEV_MODE=true`.
2. Open `/dev/ui`. It is a public route, so no login is needed.
3. Press Tab to move through the buttons, the form, and the table scroll region.
4. Switch the OS between light and dark, or turn on reduced motion, and reload.

## Driving it with verify.sh

Run this on minis only. It is not part of CI. The deps are pinned in `ui-a11y/package-lock.json` (`axe-core` 4.14.0, `playwright-core` 1.64.1). `npm ci` installs them into `/tmp/goforge-verify/ui-a11y-node` whenever the lock changes. Chromium comes from `~/.cache/ms-playwright`; to use another binary, set `GOFORGE_VERIFY_CHROMIUM`.

Host mode on minis, where the live GoForge holds `8080`, follows [SKILL.md](../SKILL.md#host-mode). Add `GOFORGE_DEV_MODE=true` to the host env file. Then:

```bash
export GOFORGE_VERIFY_BASE=http://127.0.0.1:18080
export GOFORGE_VERIFY_PSQL="sudo -n docker exec -i <pg-container> psql -U goforge -d goforge_verify -v ON_ERROR_STOP=1 -tA"
export GOFORGE_VERIFY_PROOF=/tmp/goforge-verify/proof
.cursor/skills/verify-goforge/scripts/verify.sh drive-register
.cursor/skills/verify-goforge/scripts/verify.sh drive-login
.cursor/skills/verify-goforge/scripts/verify.sh drive-ui-a11y
```

`drive-login` is optional. It creates the project the project-tab scans use. Without `register.password` in the state directory, only the public pages are scanned.

To set the schemes, use `GOFORGE_UI_SCHEMES` (default `light,dark`). To replace the route list, use `GOFORGE_UI_ROUTES`.

The drive writes `ui-a11y-verdict.txt`, `ui-a11y-report.json` (axe rule ids, impacts, and targets, with node HTML dropped because pages embed the CSRF token), and `ui-a11y/<scheme>/<route>.png`. It exits `0` when the harness ran. Every product miss, including `axe-gate=fail`, is a `fail` line. It exits `2` when the browser, the npm install, or the base URL failed. It exits `1` when `GOFORGE_VERIFY_BASE` is unset and there is no `stack.env`, which means it refuses to drive the default `8080`.

## Gotchas

- `register.password` ends in a newline. Browsers strip line breaks from a password input, so filling the login form fails. The drive posts `/auth/login` through the browser context request API with the raw file contents and the form's `csrf_token`, then navigates with that session.
- The focus ring animates (`transition-all`). The keyboard check waits 350 ms before it reads the box-shadow.
- The confirm dialog `#ui-confirm` is always in the page (closed). Selectors for an open app dialog should use `dialog[data-ui-dialog="ephemeral"]`.
- Never confirm dashboard Prune in a drive against a host with Docker access.
- `/auth/register` returns `403` once an account exists. The drive skips it on a `403`, and skips `/dev/ui` on a `404`, both noted in the verdict.
- Baseline at PR1 (branch `feat/ui-kit-foundation`): `checks=pass` in both schemes and `axe-gate=fail`. The axe findings match `main` exactly on all 28 shared scans, so they predate the token work:
  - `color-contrast`: white on `primary-600` (`.btn-primary`) is 3.76:1, `text-primary-600` links are 3.6:1, and `.btn-warning` is 2.14:1 in light and 3.18:1 in dark.
  - `/projects/new`: `label` critical and `select-name` critical.
  - Project Overview: `dlitem` serious.
  - `/services`: many catalog-card contrast nodes.
  - `/containers` passes.
  - Clearing the gate needs a palette decision, for example `.btn-primary` on `primary-700`, and the label fixes in later PRs.
- The `toggle` event of a popover fires after the key that closed it. That is why Tab in a menu focuses the trigger itself before it hides the menu, and why the drive waits a frame after each key before it reads focus.
- After Escape hides a tooltip, it stays hidden until the pointer leaves the trigger or focus moves away. Each tooltip check starts by moving the pointer to the corner and blurring.
- The sidebar nav renders twice from one templ (the desktop aside and the mobile Sheet). `app.js` marks the current link in both, and one of the two is always `display: none`, so there is one nav landmark at a time.
- In host mode there is no Docker, so the containers list has no rows. Its tooltips are covered by `TestContainerRowTooltips`.
- `GOFORGE_VERIFY_PSQL` needs `-tA`. Without it `drive-register` reads the `count` header as a user count and exits `2` with "users table is not empty".
- `#dashboard-stats` uses `animate-fade-in`. A scan that lands mid-fade can report extra `color-contrast` nodes (`dt` and the stat labels at `#717885`, 4.44:1) on `/dashboard`. Rerun before calling that a regression. On the PR6 N1 run, the first light pass caught 6 nodes and the rerun matched `main` (1 node).
- The order of the `/services` catalog changes between runs, so screenshots of that page differ even when the CSS has not changed.
