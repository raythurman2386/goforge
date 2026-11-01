package ui

import "github.com/a-h/templ"

// Overlays (Dropdown, Popover, Tooltip) render on the browser popover
// attribute, so they open in the top layer (no portal, no z-index) and get
// light dismiss from the browser. Triggers use popovertarget, so a menu or
// popover opens without JS. static/js/ui.js adds placement below or above
// the trigger (getBoundingClientRect plus a viewport flip), the menu key
// model, focus moves and focus return, aria-expanded sync on toggle, and
// the tooltip hover and focus timing. Evergreen browsers only; there is no
// polyfill.
//
// An overlay never sits inside a region that htmx polls (hx-trigger
// "every Ns") or swaps over SSE: a swap would destroy it while it is open.
// ui.js also closes every open overlay before htmx saves history.

// OverlaySide is the preferred side of the trigger. ui.js flips to the
// other side when the preferred one has no room in the viewport.
type OverlaySide string

// Overlay sides.
const (
	SideBottom OverlaySide = "bottom"
	SideTop    OverlaySide = "top"
)

// OverlayAlign lines the overlay up with the trigger's start edge, end
// edge, or center. ui.js clamps it inside the viewport.
type OverlayAlign string

// Overlay alignments.
const (
	AlignStart  OverlayAlign = "start"
	AlignEnd    OverlayAlign = "end"
	AlignCenter OverlayAlign = "center"
)

func sideOr(s, def OverlaySide) string {
	switch s {
	case SideBottom, SideTop:
		return string(s)
	}
	return string(def)
}

func alignOr(a, def OverlayAlign) string {
	switch a {
	case AlignStart, AlignEnd, AlignCenter:
		return string(a)
	}
	return string(def)
}

// TriggerID is the id of the button that opens the overlay with this id.
// The menu or popover panel is labeled by it.
func TriggerID(overlayID string) string { return overlayID + "-trigger" }

// --- Dropdown ---------------------------------------------------------------

// DropdownProps configures a DropdownTrigger and its DropdownMenu. Pass the
// same ID to both:
//
//	@ui.DropdownTrigger(ui.DropdownProps{ID: "env-menu"}) { staging }
//	@ui.DropdownMenu(ui.DropdownProps{ID: "env-menu"}) {
//		@ui.DropdownItem(ui.DropdownItemProps{Href: "/projects/1?env=2"}) { production }
//	}
type DropdownProps struct {
	// ID is the menu id, unique in the page. The trigger is TriggerID(ID).
	ID string
	// Variant and Size are the trigger's Button look (default secondary).
	Variant ButtonVariant
	Size    ButtonSize
	// Label is the trigger's aria-label. Required when the trigger has no
	// visible text (ButtonIcon).
	Label string
	// Align lines the menu up with the trigger (default start).
	Align OverlayAlign
	// Side is the preferred side (default bottom).
	Side OverlaySide
	// Class is appended to the trigger classes; MenuClass to the menu's.
	Class     string
	MenuClass string
	// Attrs pass through to the trigger button.
	Attrs templ.Attributes
}

func dropdownTriggerProps(p DropdownProps) ButtonProps {
	return ButtonProps{
		Variant: p.Variant,
		Size:    p.Size,
		Label:   p.Label,
		Class:   p.Class,
		Attrs: Attrs(templ.Attributes{
			"id":                   TriggerID(p.ID),
			"popovertarget":        p.ID,
			"aria-haspopup":        "menu",
			"aria-expanded":        "false",
			"aria-controls":        p.ID,
			"data-ui-menu-trigger": true,
		}, p.Attrs),
	}
}

// dropdownMenuClass is the menu panel. Position, the open transition, and
// reduced motion live in input.css ([data-ui-overlay]).
func dropdownMenuClass() string {
	return "min-w-[14rem] max-w-[calc(100vw-1rem)] rounded-xl border border-divider bg-card p-1.5 text-left shadow-xl ring-1 ring-border"
}

func dropdownMenuAttrs(p DropdownProps) templ.Attributes {
	return Attrs(templ.Attributes{
		"id":               p.ID,
		"popover":          "auto",
		"role":             "menu",
		"aria-orientation": "vertical",
		"aria-labelledby":  TriggerID(p.ID),
		"tabindex":         "-1",
		"data-ui-overlay":  "menu",
		"data-ui-menu":     true,
		"data-ui-side":     sideOr(p.Side, SideBottom),
		"data-ui-align":    alignOr(p.Align, AlignStart),
		"class":            dropdownMenuClass(),
	}, templ.Attributes{"class": p.MenuClass})
}

// DropdownItemTone is the look of a menu item.
type DropdownItemTone string

// Menu item tones.
const (
	ItemDefault DropdownItemTone = "default"
	ItemPrimary DropdownItemTone = "primary"
	ItemDanger  DropdownItemTone = "danger"
)

// DropdownItemProps configures one menu item: a link when Href is set,
// otherwise a <button type="button"> (pass hx-* in Attrs).
type DropdownItemProps struct {
	Href  templ.SafeURL
	Tone  DropdownItemTone
	Class string
	Attrs templ.Attributes
}

func dropdownItemClass(t DropdownItemTone) string {
	switch t {
	case ItemPrimary:
		return "block w-full rounded-lg px-3 py-2 text-left text-sm font-bold text-primary-700 outline-none transition-colors hover:bg-primary-50 focus:bg-primary-50 dark:text-primary-300 dark:hover:bg-primary-900/30 dark:focus:bg-primary-900/30"
	case ItemDanger:
		return "block w-full rounded-lg px-3 py-2 text-left text-sm font-medium text-danger outline-none transition-colors hover:bg-danger/10 focus:bg-danger/10"
	case ItemDefault:
	}
	// ItemDefault, the zero value, and unknown values.
	return "block w-full rounded-lg px-3 py-2 text-left text-sm font-medium text-foreground-secondary outline-none transition-colors hover:bg-primary-50 hover:text-primary-700 focus:bg-primary-50 focus:text-primary-700 dark:hover:bg-primary-900/30 dark:hover:text-primary-300 dark:focus:bg-primary-900/30 dark:focus:text-primary-300"
}

func dropdownItemAttrs(p DropdownItemProps) templ.Attributes {
	return Attrs(templ.Attributes{
		"role":             "menuitem",
		"tabindex":         "-1",
		"data-ui-menuitem": true,
		"class":            dropdownItemClass(p.Tone),
	}, templ.Attributes{"class": p.Class}, p.Attrs)
}

// --- Popover ----------------------------------------------------------------

// PopoverProps configures a PopoverTrigger and its Popover: a non-modal
// disclosure panel (role="dialog") on popover="auto". Light dismiss and
// Escape close it; focus moves into it on open and back to the trigger on
// close. Pass the same ID to both.
type PopoverProps struct {
	// ID is the panel id, unique in the page. The trigger is TriggerID(ID).
	ID string
	// Title is the panel's accessible name and visible heading.
	Title string
	// Trigger look; Label is the trigger aria-label (required for ButtonIcon).
	Variant ButtonVariant
	Size    ButtonSize
	Label   string
	Align   OverlayAlign
	Side    OverlaySide
	// Class is appended to the trigger classes; PanelClass to the panel's.
	Class      string
	PanelClass string
	// Attrs pass through to the trigger button.
	Attrs templ.Attributes
}

// PopoverTitleID is the id of a popover's heading.
func PopoverTitleID(popoverID string) string { return popoverID + "-title" }

func popoverTriggerProps(p PopoverProps) ButtonProps {
	return ButtonProps{
		Variant: p.Variant,
		Size:    p.Size,
		Label:   p.Label,
		Class:   p.Class,
		Attrs: Attrs(templ.Attributes{
			"id":                      TriggerID(p.ID),
			"popovertarget":           p.ID,
			"aria-haspopup":           "dialog",
			"aria-expanded":           "false",
			"aria-controls":           p.ID,
			"data-ui-popover-trigger": true,
		}, p.Attrs),
	}
}

func popoverPanelClass() string {
	return "w-80 max-w-[calc(100vw-1rem)] rounded-xl border border-border bg-card p-4 text-left text-sm text-foreground shadow-xl"
}

func popoverAttrs(p PopoverProps) templ.Attributes {
	return Attrs(templ.Attributes{
		"id":              p.ID,
		"popover":         "auto",
		"role":            "dialog",
		"aria-labelledby": PopoverTitleID(p.ID),
		"tabindex":        "-1",
		"data-ui-overlay": "popover",
		"data-ui-side":    sideOr(p.Side, SideBottom),
		"data-ui-align":   alignOr(p.Align, AlignStart),
		"class":           popoverPanelClass(),
	}, templ.Attributes{"class": p.PanelClass})
}

// --- Tooltip ----------------------------------------------------------------

// TooltipProps configures a Tooltip: a short text that describes its
// trigger, on popover="manual". ui.js shows it after a delay on hover and
// on keyboard focus, keeps it open while the pointer is over it (WCAG
// 1.4.13), and hides it on Escape without moving focus. Render it next to
// the trigger and put TooltipAttrs(ID) on the trigger.
//
// A tooltip only adds a description (aria-describedby). It is never the
// accessible name: an icon button still needs ButtonProps.Label.
type TooltipProps struct {
	// ID is the tooltip id, unique in the page.
	ID string
	// Text is the tooltip content.
	Text string
	// Side defaults to top; Align defaults to center.
	Side  OverlaySide
	Align OverlayAlign
	Class string
}

// TooltipAttrs wire a trigger to the tooltip with this id. The trigger
// must be focusable (a button, a link, or tabindex="0").
func TooltipAttrs(tooltipID string) templ.Attributes {
	return templ.Attributes{
		"aria-describedby": tooltipID,
		"data-ui-tooltip":  tooltipID,
	}
}

func tooltipClass() string {
	return "max-w-xs rounded-md bg-foreground px-2.5 py-1.5 text-xs font-medium text-background shadow-lg"
}

func tooltipAttrs(p TooltipProps) templ.Attributes {
	return Attrs(templ.Attributes{
		"id":              p.ID,
		"popover":         "manual",
		"role":            "tooltip",
		"data-ui-overlay": "tooltip",
		"data-ui-side":    sideOr(p.Side, SideTop),
		"data-ui-align":   alignOr(p.Align, AlignCenter),
		"class":           tooltipClass(),
	}, templ.Attributes{"class": p.Class})
}
