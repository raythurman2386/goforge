package ui

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestDropdownTriggerARIA(t *testing.T) {
	got := render(t, DropdownTrigger(DropdownProps{ID: "m1", Attrs: templ.Attributes{"data-testid": "x"}}), "Menu")
	assertHas(t, got,
		`<button type="button"`, `id="m1-trigger"`, `popovertarget="m1"`, `aria-haspopup="menu"`,
		`aria-expanded="false"`, `aria-controls="m1"`, `data-ui-menu-trigger`, `data-testid="x"`, `class="btn-secondary"`)
	assertLacks(t, got, `aria-expanded="true"`, "onclick")

	icon := render(t, DropdownTrigger(DropdownProps{ID: "m2", Size: ButtonIcon, Label: "More actions"}), "<svg aria-hidden=\"true\"></svg>")
	assertHas(t, icon, `aria-label="More actions"`, `btn-icon`)
}

func TestDropdownMenuAndItems(t *testing.T) {
	menu := render(t, DropdownMenu(DropdownProps{ID: "m1", Align: AlignEnd, MenuClass: "w-64"}), "items")
	assertHas(t, menu,
		`id="m1"`, `popover="auto"`, `role="menu"`, `aria-labelledby="m1-trigger"`, `aria-orientation="vertical"`,
		`tabindex="-1"`, `data-ui-overlay="menu"`, `data-ui-menu`, `data-ui-side="bottom"`, `data-ui-align="end"`, "w-64", "rounded-xl")

	link := render(t, DropdownItem(DropdownItemProps{Href: "/projects/p1?env=e1"}), "prod")
	assertHas(t, link, `<a href="/projects/p1?env=e1"`, `role="menuitem"`, `tabindex="-1"`, `data-ui-menuitem`)

	btn := render(t, DropdownItem(DropdownItemProps{Tone: ItemDanger, Attrs: templ.Attributes{"hx-post": "/x"}}), "Delete")
	assertHas(t, btn, `<button type="button"`, `role="menuitem"`, `hx-post="/x"`, "text-danger")

	sep := render(t, DropdownSeparator(), "")
	assertHas(t, sep, `role="separator"`)
}

func TestOverlayDefaults(t *testing.T) {
	if got := sideOr("", SideBottom); got != "bottom" {
		t.Errorf("side default = %q", got)
	}
	if got := sideOr("left", SideTop); got != "top" {
		t.Errorf("unknown side = %q", got)
	}
	if got := alignOr("", AlignCenter); got != "center" {
		t.Errorf("align default = %q", got)
	}
	for _, tone := range []DropdownItemTone{ItemDefault, ItemPrimary, ItemDanger, ""} {
		if dropdownItemClass(tone) == "" {
			t.Errorf("dropdownItemClass(%q) is empty", tone)
		}
	}
}

func TestPopoverARIA(t *testing.T) {
	p := PopoverProps{ID: "help", Title: "About backups", Size: ButtonIcon, Label: "About backups", Variant: ButtonGhost}
	trigger := render(t, PopoverTrigger(p), "?")
	assertHas(t, trigger,
		`id="help-trigger"`, `popovertarget="help"`, `aria-haspopup="dialog"`, `aria-expanded="false"`,
		`aria-controls="help"`, `data-ui-popover-trigger`, `aria-label="About backups"`)

	panel := render(t, Popover(p), "<p>Body</p>")
	assertHas(t, panel,
		`id="help"`, `popover="auto"`, `role="dialog"`, `aria-labelledby="help-title"`, `id="help-title"`,
		`data-ui-overlay="popover"`, `popovertargetaction="hide"`, `data-ui-popover-close`, `aria-label="Close"`, "<p>Body</p>")
	assertLacks(t, panel, `aria-modal`)
}

func TestTooltipARIA(t *testing.T) {
	tip := render(t, Tooltip(TooltipProps{ID: "tip-stop", Text: "Stop <now>"}), "")
	assertHas(t, tip,
		`id="tip-stop"`, `popover="manual"`, `role="tooltip"`, `data-ui-overlay="tooltip"`,
		`data-ui-side="top"`, `data-ui-align="center"`, "Stop &lt;now&gt;")

	a := TooltipAttrs("tip-stop")
	if a["aria-describedby"] != "tip-stop" || a["data-ui-tooltip"] != "tip-stop" {
		t.Errorf("TooltipAttrs = %v", a)
	}

	// A tooltip describes; the icon button keeps its own accessible name.
	btn := render(t, Button(ButtonProps{Size: ButtonIcon, Label: "Stop web", Attrs: TooltipAttrs("tip-stop")}), "<svg aria-hidden=\"true\"></svg>")
	assertHas(t, btn, `aria-label="Stop web"`, `aria-describedby="tip-stop"`, `data-ui-tooltip="tip-stop"`)
	if strings.Contains(btn, "title=") {
		t.Error("icon button should not fall back to title")
	}
}
