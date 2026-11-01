# UI kit reference

GoForge pages are built from one templ component kit in `internal/web/templates/components/ui` (package `ui`). It runs on Tailwind 3.4.17, htmx 2, native `<dialog>`, and the native `popover` attribute. One delegated script, `static/js/ui.js`, adds keyboard and focus behavior. There is no React, no JS build step, and no client framework.

**Rule:** pages and shared components use `components/ui`. Do not write `class="btn…"`, `class="card…"`, `class="input"`, `class="select"`, or `class="label"` by hand. Do not add `confirm(...)` or the old `id="modal"` and `id="menu-button"` contracts. `TestPagesUseKit` in `internal/web/templates/components/sweep_gate_test.go` fails on any of those outside `components/ui`. Do not add inline event handlers (any `on…=` attribute, an `"on…":` key in `templ.Attributes`, or `hx-on`) or an inline `<script>` anywhere, `components/ui` included. `TestNoInlineHandlersOrScripts` in the same file fails on them; page behavior goes in a delegated `data-*` listener in `static/js/app.js`.

## Gallery

With `GOFORGE_DEV_MODE=true`, `/dev/ui` renders every token and component, with keyboard demos for the dialog, the confirm, the overlays, the tabs, the tables, and the sheet. Without dev mode the route returns `404`. The `verify.sh drive-ui-a11y` drive (`.cursor/skills/verify-goforge/features/ui-a11y.md`) scans the gallery and the main pages with axe in light and dark, and runs the keyboard scripts.

## Shared helpers

| Helper | Use |
| --- | --- |
| `ui.Class(parts...)` | Joins literal class lists, drops blanks and duplicates. Keep every part a complete literal so Tailwind and `class_check.py` can see it. |
| `ui.Attrs(sets...)` | Merges `templ.Attributes`. A later `class` is appended, any other key overrides. |
| `ui.VisuallyHidden` | `sr-only` text for screen readers. |
| `ui.ButtonClass(variant, size)` | The button look for the rare element that cannot be a `ui.Button` (the current page in pagination). |
| `ui.LabelClass(extra)` | The label look for a `<legend>`, or a `<p>` naming read-only content, where `<label for>` does not fit. Form controls use `ui.Label` or `ui.Field`. |
| `ui.OpenDialogAttrs(url)` | `hx-get` a Dialog fragment into `<body>` with `aria-haspopup="dialog"`. |
| `ui.ConfirmAttrs(question, tone, label)` | `hx-confirm` through the kit confirm dialog. |
| `ui.OpenSheetAttrs(id)` | A button that opens a static Sheet. |
| `ui.TooltipAttrs(id)` | Links a trigger to a Tooltip with `aria-describedby`. |
| `ui.CopyAttrs(id)` | A copy button: copies the text of element `id`. |

Every component takes `Class` (appended) and `Attrs` (`hx-*`, `data-*`, `aria-*`, `id` passthrough) unless noted.

## Components

The 20 components of Path A, with their variants.

### Button

`ui.Button(ui.ButtonProps{...}) { text }` renders `<button type="button">` unless `Type` is `"submit"` or `"reset"`, or `<a href>` when `Href` is set. A disabled link renders a non-focusable `<span aria-disabled="true">`.

- Variants: `ButtonPrimary`, `ButtonSecondary` (the zero value), `ButtonDanger`, `ButtonWarning`, `ButtonGhost`, `ButtonGhostPrimary`, `ButtonGhostWarning`, `ButtonGhostDanger`, `ButtonLink`.
- Sizes: `ButtonMD` (default), `ButtonSM`, `ButtonIcon` (square, 44 px below `md` and 32 px from `md` up), `ButtonInline` (no padding, for `ButtonLink` in text).
- Below `md` every button except `ButtonInline` is at least 44 px tall (`min-h-11` on `.btn`). From `md` up the padding sets the height again, so `ButtonSM` is about 32 px on desktop.
- `ButtonIcon` needs `Label`, which becomes `aria-label`. The icon `svg` gets `aria-hidden="true"`.
- Use `Href` only for navigation, never for an action that changes state.

### Input, Textarea, Select

`ui.Input`, `ui.Textarea` (`Rows`, default 4), and `ui.Select` (`Options []SelectOption`, `Blank`, `BlankLabel`; `ui.StringOptions` builds options from strings) render a label, the control, help, and an error. Below `md` the control is at least 44 px tall (`min-h-11` on `.input`, which `.select` builds on).

- The control id is `ID`, or `"field-" + Name` when `ID` is empty. It is never the bare name, so two forms on one page do not collide. Give an explicit form-prefixed `ID` (for example `login-email`) where two forms share a page.
- `Help` renders `<p id="<id>-help">` and `Error` renders `<p id="<id>-error">`. Both are listed in `aria-describedby`, help first, then error, then `DescribedBy`.
- `Error` sets `aria-invalid="true"`, which turns the control red.
- `Required` sets the native `required` and, on the label, a visual `*` (`aria-hidden`) plus "(required)" for screen readers.
- `Autocomplete`, `Placeholder`, and `Disabled` map to the native attributes. Select stays a native `<select>`; there is no custom listbox.

### Label and Field

`ui.Label(ui.LabelProps{For, Required})` is a `<label for>`. `ui.Field(ui.FieldProps{ID, Label, Required, Help, Error})` wraps a control that the page renders itself (for example a `CheckboxControl` row or a `<input type="time">` with extra attributes) with the same label, help, and error wiring.

### Checkbox and Switch

`ui.Checkbox(ui.CheckboxProps{...})` renders a native checkbox with its label to the right and an optional description. `ui.CheckboxControl` is the bare box for a page that supplies its own label. `ui.CheckboxGroup(ui.CheckboxGroupProps{Legend, Required, DescribedBy})` wraps related boxes in `<fieldset>` and `<legend>`.

- `HiddenFalse` adds the hidden `false` input after the box, so an unchecked box still posts a value, for the forms that read it.
- `ui.Switch` is the same native checkbox with `role="switch"`, drawn as a track. The form still posts the checkbox value. The unchecked track is at least 3:1 against the card.

### Badge

`ui.Badge(ui.BadgeProps{Tone, Variant, Size, Square})` is text, never color alone.

- Tones: `BadgeNeutral`, `BadgePrimary`, `BadgeSuccess`, `BadgeWarning`, `BadgeDanger`, `BadgeInfo`.
- Variants: `BadgeSoft`, `BadgeSubtle`, `BadgeSolid`, `BadgeOutline`. Sizes: `BadgeSM`, `BadgeMD`.
- `components.StatusBadge`, `DeploymentStatusBadge`, and `TriggerTypeBadge` wrap it, so SSE handlers keep their signatures.

### Card

`ui.Card(ui.CardProps{Flush, Href})` with `CardHeader`, `CardTitle(level, attrs)`, `CardDescription`, `CardContent`, and `CardFooter`.

- `Flush` drops the padding for a card made of a header, content, a list, or a table edge to edge.
- `CardTitle` takes the heading level (2, 3, or 4; default 3). Pick the level that fits the page outline; the look does not change.
- `Href` makes the whole card a link.

### Alert

`ui.Alert(ui.AlertProps{Tone, Title, Dismissible}) { description }` and `ui.AlertMessage(tone, message)`.

- Tones: `AlertInfo`, `AlertSuccess`, `AlertWarning`, `AlertError`.
- `AlertError` is `role="alert"`; the other tones are `role="status"`.
- `Dismissible` adds a 44 px (below `md`) close button with `aria-label="Dismiss"`; `ui.js` removes the alert.

### Table

`ui.Table(ui.TableProps{ID, Caption, CaptionVisible, Compact, Fixed})` with `THead`, `TBody`, `TR`, `TH(ui.CellProps{...})`, `TD(ui.CellProps{...})`, and `TableEmpty(colspan)`.

- The table sits in a horizontal scroll wrapper with `role="region"`, `tabindex="0"`, and `aria-labelledby` pointing at the caption, so keyboard users can scroll it.
- `Caption` is required. It is `sr-only` unless `CaptionVisible` is set. `TH` sets `scope` (`col` by default, or `row`). `CellEnd` right-aligns a cell.

### NavTabs and Tabs

- `ui.NavTabs` and `ui.NavTab` are server-routed tabs: links in a labeled `<nav>`, with `aria-current="page"` on the current route and no tab roles. Project detail uses them.
- `ui.Tabs`, `TabList`, `Tab`, and `TabPanel` are in-page panels: `tablist`, `tab`, and `tabpanel` roles, ids from `ui.TabID` and `ui.TabPanelID`, and a roving `tabindex`. Arrow keys, Home, and End move and select (automatic activation). Tab moves into the selected panel.

### Dialog and confirm

`ui.Dialog(ui.DialogProps{ID, Title, Description, NonDismissible, Static})` is a native `<dialog>` that `ui.js` opens with `showModal()`.

- An empty `ID` gets a generated unique id, so two dialogs can stack. The title is the accessible name (`aria-labelledby`), and `Description` becomes `aria-describedby`.
- Server fragments are ephemeral. They are appended to `<body>` by a trigger using `ui.OpenDialogAttrs`, opened on `htmx:load`, and removed on close. `Static` dialogs stay in the page and open from a `[data-ui-dialog-open="id"]` button.
- Focus moves into the dialog, Tab stays inside, and Escape closes it. Focus returns to the opener. A backdrop click closes it unless `NonDismissible` is set. A successful request from inside closes it. A failed one writes its text into the dialog's own `role="alert"` error node (`ui.DialogErrorID`).
- `ui.ConfirmDialog` is rendered once by `layouts.Base`. `ui.js` opens it as a `role="alertdialog"` for every `hx-confirm` trigger, and for a plain form submit with `hx-confirm`. Cancel, Escape, and the backdrop send nothing. Confirm sends one request. `data-ui-confirm-tone="danger"` and `data-ui-confirm-label` set the accept button.

### Sheet

`ui.Sheet(ui.SheetProps{ID, Title, Description, Side, CloseAt})` is a Dialog pinned to the left (`SheetLeft`, default) or right (`SheetRight`) edge at full height. It opens from a button with `ui.OpenSheetAttrs(id)`, which carries `aria-expanded`. It traps focus and closes on Escape, and focus returns to the opener. `CloseAt` is a media query that closes an open sheet, for example `(min-width: 768px)` for the mobile navigation sheet, whose links the desktop sidebar shows inline.

### Dropdown

`ui.DropdownTrigger`, `ui.DropdownMenu`, `ui.DropdownItem` (tones `ItemDefault`, `ItemPrimary`, `ItemDanger`), and `ui.DropdownSeparator`, on the `popover` attribute.

- The trigger carries `aria-haspopup="menu"` and `aria-expanded`. The panel is `role="menu"` with `role="menuitem"` items.
- Enter, Space, ArrowDown, or ArrowUp opens it. Arrows wrap, Home and End jump, and typeahead finds an item. Escape closes it and focuses the trigger. Tab closes it and moves on.
- Placement: `SideBottom` (default) or `SideTop`, and `AlignStart`, `AlignEnd`, or `AlignCenter`. The panel flips when there is no room.

### Popover

`ui.PopoverTrigger` and `ui.Popover(ui.PopoverProps{ID, Title, ...})` (same `ID` on both) open a non-modal `role="dialog"` panel on `popover="auto"`, labeled by its heading (`ui.PopoverTitleID`), with a close button. The trigger carries `aria-haspopup="dialog"`, `aria-controls`, and `aria-expanded`. Focus moves into the panel on open. Escape and an outside click close it and return focus to the trigger. It works without JS through `popovertarget`.

### Tooltip

`ui.Tooltip(ui.TooltipProps{ID, Text})` plus `ui.TooltipAttrs(id)` on the trigger. It shows after a short delay on hover or focus, describes the trigger with `aria-describedby`, stays open while the pointer is over it (WCAG 1.4.13), and hides on Escape without moving focus. It never names a control. An icon button keeps its own `aria-label`.

### Breadcrumb

`ui.Breadcrumb([]ui.Crumb{...})` renders `<nav aria-label="Breadcrumb">` with an ordered list. The last crumb has `aria-current="page"`, and the separators are `aria-hidden`.

### Avatar

`ui.Avatar(ui.AvatarProps{Name, Size, Label})` shows initials (`ui.Initials`) only, with no remote images. Sizes: `AvatarSM`, `AvatarMD`, `AvatarLG`. With no `Label` it is decorative (`aria-hidden`), for use next to a visible name; with `Label` it is `role="img"` with that accessible name.

### Skeleton

`ui.Skeleton(shape, class)` with `SkeletonLine`, `SkeletonCircle`, or `SkeletonBlock`. Skeletons are `aria-hidden`. The region they fill carries `role="status"`, an `aria-label`, and `aria-busy="true"`. The pulse stops under reduced motion.

## `static/js/ui.js` contract

`layouts.Base` loads `htmx.min.js`, `htmx-sse.js`, `app.js`, then `ui.js`. `app.js` keeps the shell: toasts (including the delegated `[data-toast-dismiss]` button), htmx error toasts, and the active sidebar link. `ui.js` is the kit runtime.

1. **Delegated listeners only.** `click`, `keydown`, `focusin`, `pointerover`, and `toggle` are attached to `document` once. Targets are found by `data-ui*` attributes, so nothing binds to an element htmx may replace.
2. **Init once.** Markup that needs setup is `data-ui="<name>"`. It is initialized on the first load and on every `htmx:load`, then marked `data-ui-ready`, so a second pass is a no-op. Components register with `GoForgeUI.register(name, {init, events})`.
3. **Overlays outside polled regions.** Dialogs go to `<body>` and popovers to the top layer. Before htmx snapshots the page for history, every open dialog and popover closes. A swap that replaces an open overlay's anchor closes it first, so focus returns while the trigger exists.
4. **Toasts stay in `app.js`.** Do not duplicate `LEVEL_CLASSES` in `ui.js`.
5. **Literal classes.** Every class string in `ui.js` and `app.js` is a complete literal. Both files are in `tailwind.config.js` `content` and in `scripts/class_check.py`.
6. **Motion** respects `prefers-reduced-motion` (CSS in `static/css/input.css`).

The attributes `ui.js` reads:

| Attribute | Meaning |
| --- | --- |
| `data-ui-dialog` (`ephemeral` or `static`), `data-ui-dialog-close`, `data-ui-dialog-open`, `data-ui-dialog-error`, `data-ui-dialog-nondismissible` | Dialog |
| `hx-confirm`, `data-ui-confirm-tone`, `data-ui-confirm-label` | Kit confirm dialog |
| `data-ui-sheet`, `data-ui-sheet-close-at` | Sheet |
| `data-ui-overlay`, `data-ui-menu`, `data-ui-menu-trigger`, `data-ui-menuitem`, `data-ui-side`, `data-ui-align`, `data-ui-popover-close`, `data-ui-tooltip` | Dropdown, Popover, Tooltip |
| `data-ui-tab` | Tabs |
| `data-ui-dismiss` | Dismissible Alert |
| `data-ui-copy="<id>"` | Copy button. It copies the text of element `<id>` and reads "Copied!" (or "Copy failed") for two seconds. The copied text stays in the page, never in an attribute. |

`window.GoForgeUI` exposes `register`, `init`, `closeOverlays`, `place`, `openDialog`, and `confirm`.

## Styling

Tokens are CSS variables in `static/css/input.css` (`--gf-*`, RGB triplets with light and dark values under `prefers-color-scheme`). `tailwind.config.js` maps them to utilities such as `bg-card`, `text-muted-foreground`, `border-field-border`, `ring-ring`, and `rounded-ui`. Dark mode is OS-only; there is no toggle.

The variant classes (`.btn-*`, `.input`, `.select`, `.label`, `.checkbox`, `.switch`, `.card*`, `.skeleton-*`) live in the components layer of `input.css` and are emitted only by the kit's class helpers. A caller's `Class` utility always wins over them. `.ui-focus-ring` is the shared `focus-visible` ring.

Text must meet 4.5:1. Filled buttons use the `-700` shade with white text (`.btn-primary` on `primary-700`, `.btn-warning` on `amber-700`), ghost buttons rest on `text-gray-500` (dark `gray-400`), and primary text uses `text-primary-700` (dark `primary-400`). Keep light `text-gray-400` for icons, not text. The ratios are in comments beside each class in `input.css`.

## Accessibility expectations

Every change to a page or component keeps these:

- axe finds no serious or critical issues on the page in light or dark (`drive-ui-a11y`). The baseline is zero on every scan, including the service, container, and deployment detail pages and `/settings/activity`; keep it there.
- Every control has a visible label or, for icon-only buttons, an `aria-label`. A tooltip is never the only name.
- Icon-only buttons are at least 44 by 44 px below `md` (`ButtonIcon`, the Alert and toast dismiss buttons). Text buttons, inputs, selects, and textareas are at least 44 px tall below `md` (`ButtonInline` and checkboxes, whose label is the target, are exempt). The drive's `touch-targets` checks measure it at 375 px.
- Keyboard focus is always visible (`focus-visible` ring) and never lost. Overlays move focus in, trap it when modal, close on Escape, and return focus to the opener.
- Help and errors are tied to their control with `aria-describedby`; errors set `aria-invalid`.
- Status is text, not color alone (Badge, Alert). Errors use `role="alert"`; other live updates use `role="status"`.
- Polling and SSE regions never contain an overlay.
- Template tests next to each component assert the ARIA wiring (`components/ui/*_test.go`).

## Page scripts

Pages carry no inline `<script>` and no inline event handlers. The few page behaviors the kit does not cover live in `static/js/app.js` as delegated listeners keyed on `data-*` attributes, so they survive htmx swaps and are ready for a strict `script-src` CSP:

- `[data-git-source-toggle]` (`components/git_browser.templ`, the project form): on change, and on load or swap, it shows either the manual repository URL input or the git source repository select, and gives exactly one of them the `repository_url` name.
- `containerStopped` and `containerRestarted` (`pages/containers/detail.templ`): the stop and restart responses send these as `HX-Trigger`; `app.js` copies the matching `<template id="container-status-{state}">` badge into `#container-status`.
- `[data-secret]` with `[data-secret-action="toggle"|"copy"]` (`pages/services/detail.templ`): Show and Copy for masked credentials. The value is fetched from `data-secret-url` on demand, so it is never rendered into the page.

One inline-code form remains and is out of scope until a CSP goal: `hx-vals="js:{...}"` on the repository select in `git_browser.templ` (htmx evaluates it).
