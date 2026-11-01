# UI kit foundation and accessibility

The UI reads its colors, radius, and focus ring from CSS variable tokens. Light and dark values follow `prefers-color-scheme`. A keyboard user sees a focus ring on buttons, links, and controls, but a mouse click does not draw one. With reduced motion requested, transitions and animations stop, except the loading spinner. `static/js/ui.js` initializes `data-ui` components once per element, including inside content that htmx swaps in. With `GOFORGE_DEV_MODE=true`, `/dev/ui` shows a gallery of the tokens, the kit components, and the legacy classes (`.btn*`, `.input`, `.label`, `.card*`). Without dev mode it returns `404`.

## Sub-features

- `token-scheme`: `--gf-background` on `:root` is `238 238 238` in light (GrokDay `#eeeeee`) and `20 20 20` in dark (GrokNight `#141414`), the t1734u palette.
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
  - `dialog-axe`: axe inside the open dialog and the open confirm finds nothing serious or critical.
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
  - `overlays-axe`, `overlays-no-native-prompt`: inside the open overlays, axe finds nothing serious or critical, and no `window.confirm` appears.
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
- `axe`: axe-core scans each page for WCAG 2.0/2.1 A and AA and 2.2 AA (`wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`, `wcag22aa`), in light and dark. The public pages are `/login`, `/auth/register`, `/dev/ui`, and a missing page. With a password, it also scans `/dashboard`, `/projects`, `/projects/new`, `/services`, `/services/postgres/configure`, `/containers`, `/settings/profile` (which holds the backup restore points and the notifier form with its SMTP fields), `/settings/git`, `/settings/activity`, the first project's Overview, Deployments, Settings, and Services tabs, and the detail pages: service detail and edit, container detail, and deployment detail. `axe-gate=pass` means zero serious or critical findings.
- Detail pages and page hooks (t1681u). Ids come from `GOFORGE_UI_SERVICE_ID`, `GOFORGE_UI_CONTAINER_ID`, and `GOFORGE_UI_DEPLOYMENT_PATH` (`/projects/{id}/deployments/{id}`), or else from the first matching link on `/services`, `/containers`, and the first project's Deployments tab. The verdict prints `detail-ids <scheme> service=yes|none …`; a missing id skips its routes and checks with a `skip` line:
  - `app-git-source-toggle`: on `/projects/new` the git source select carries `data-git-source-toggle` and no `on*` attribute, the manual URL input is named `repository_url` and required, and the repository select is hidden and renamed.
  - `app-container-status-swap`: on container detail, a `containerStopped` event on `<body>` swaps the badge to exited and `containerRestarted` to running. No Docker call; the page has no inline script.
  - `app-secret-show-hide`: on service detail, Enter on Show reveals the first masked row (`data-revealed="true"`, button `Hide`) and Enter again re-masks it. The value is never written to evidence.
  - `app-typed-delete` (#107): Enter on Delete opens `#delete-service-dialog` with focus inside, 14 Tab and Shift+Tab presses stay inside, axe inside the open dialog finds nothing serious or critical, an empty name sends no `DELETE` (required field), a wrong name sends one `DELETE` that the server refuses and the dialog shows the error, Escape closes it, and the page stays. The drive never types the real name.
  - `app-protected-switch` (#107): Space on `#service-protected-switch` (`role="switch"`) flips it and the card swap keeps it; a second Space flips it back. axe inside `#service-protected` is clean.
  - `app-restore-points` (#104/#105): with an admin and a backed-up template, `#service-restore-points` loads (table, empty, or error state) and axe inside it is clean.
  - `app-agent-actions-card` (t1683u): on `/dashboard`, the Recent agent actions card with no rows has no table and no header "View all"; its empty message is followed by a "View all activity" link to `/settings/activity`. With rows, the header keeps "View all" (`?source=agent`) above the table. axe inside the card is clean.
  - `touch-targets /settings/profile`, `/projects/new`, `/dashboard`: at 375 px every visible text button, input, select, and textarea is at least 44 px tall (not `btn-inline`, not checkboxes); at the default desktop viewport every `btn-sm` is still under 44 px.

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

Host mode has no Docker, so no service, container, or deployment exists unless you seed one. Seed rows into the throwaway database only (plaintext `config` is read as legacy unsealed JSON; use a random fixture value, never a real secret), then pass the container id, which the containers list cannot show without Docker:

```bash
PG="sudo -n docker exec -i <pg-container> psql -U goforge -d goforge_verify -v ON_ERROR_STOP=1 -tA"
FIX=$(openssl rand -hex 12); DID=$(openssl rand -hex 32)
$PG <<SQL
insert into service_instances (user_id,template_name,name,version,config,status,connection_info)
  values ((select id from users where role='admin'),'postgres','ui-a11y-pg','17',
  '{"database":"app","username":"pgadmin","password":"$FIX"}','running','{"host":"goforge-service-ui-a11y","port":"5432"}');
insert into containers (service_instance_id,docker_id,name,image,status)
  values ((select id from service_instances where name='ui-a11y-pg'),'$DID','goforge-service-ui-a11y','postgres:17','running');
insert into deployments (environment_id,status,trigger_type,branch,commit_message,error_message,started_at,finished_at)
  values ((select id from environments limit 1),'failed','manual','main','ui-a11y fixture','fixture: no Docker in host mode',now()-interval '2 minutes',now()-interval '1 minute');
SQL
GOFORGE_UI_CONTAINER_ID=$DID .cursor/skills/verify-goforge/scripts/verify.sh drive-ui-a11y
```

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
  - Since `fix/axe-primary-palette-contrast`, `.btn-primary` rests on `primary-700` and primary text links use `text-primary-700`, so the white-on-`#059669` and `#059669`-text nodes are gone (72 nodes to 0 on the 2026-10-30 host run). The remaining `color-contrast` nodes are `.btn-warning` and gray muted text (`#9ca3af` light, `#6b7280` dark), plus `dlitem` on project Overview.
  - Since `fix/axe-leftovers-and-docs`, `.btn-warning` rests on `amber-700`, ghost buttons and muted text use `gray-500` (dark `gray-400`), and the Overview pairs sit in a `<dl>`. The 2026-10-30 host run went from 22 serious nodes on `main` (6 `color-contrast`, 16 `dlitem`) to 0 issues of any impact, and `axe-gate=pass` in light and dark. The baseline is zero; `dialog-axe` and `overlays-axe` no longer exempt `color-contrast`.
- The `toggle` event of a popover fires after the key that closed it. That is why Tab in a menu focuses the trigger itself before it hides the menu, and why the drive waits a frame after each key before it reads focus.
- After Escape hides a tooltip, it stays hidden until the pointer leaves the trigger or focus moves away. Each tooltip check starts by moving the pointer to the corner and blurring.
- The sidebar nav renders twice from one templ (the desktop aside and the mobile Sheet). `app.js` marks the current link in both, and one of the two is always `display: none`, so there is one nav landmark at a time.
- In host mode there is no Docker, so the containers list has no rows. Its tooltips are covered by `TestContainerRowTooltips`. Container detail still renders for a seeded `containers` row owned through its service (pass `GOFORGE_UI_CONTAINER_ID`).
- The first t1681u run found three pre-existing serious issues on pages the drive had never scanned: `dlitem` on service detail Connection Details (the rows had no `<dl>`), and `color-contrast` on the light-mode log placeholders on container and deployment detail (`text-gray-500` on the `gray-900` log pane, 3.66:1). Both are fixed in the same PR (`<dl>`, `text-gray-400`).
- `GOFORGE_VERIFY_PSQL` needs `-tA`. Without it `drive-register` reads the `count` header as a user count and exits `2` with "users table is not empty".
- `#dashboard-stats` uses `animate-fade-in`. A scan that lands mid-fade can report extra `color-contrast` nodes (`dt` and the stat labels at `#717885`, 4.44:1) on `/dashboard`. Rerun before calling that a regression. On the PR6 N1 run, the first light pass caught 6 nodes and the rerun matched `main` (1 node). Since t1681u, `scan()` waits (up to 2 s) for every finite animation to finish before it runs axe, so this should no longer show; if it does, check for a new or longer animation.
- The order of the `/services` catalog changes between runs, so screenshots of that page differ even when the CSS has not changed.
