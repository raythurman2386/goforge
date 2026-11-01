package ui

import "github.com/a-h/templ"

// SheetSide is the screen edge a Sheet slides in from.
type SheetSide string

// Sheet sides.
const (
	SheetLeft  SheetSide = "left"
	SheetRight SheetSide = "right"
)

// SheetProps configures Sheet. A Sheet is a static Dialog (the same
// <dialog> runtime in static/js/ui.js: showModal focus trap, inert page,
// Escape, backdrop click, focus return to the opener) pinned to one edge
// and full height, sliding in unless reduced motion is requested. Open it
// with a button carrying OpenSheetAttrs(id).
type SheetProps struct {
	// ID is the <dialog> id, unique in the page. Required: the opener
	// points at it.
	ID string
	// Title is the visible heading and the sheet's accessible name.
	Title string
	// Description, when set, renders under the title (aria-describedby).
	Description string
	// Side is the edge (default left).
	Side SheetSide
	// CloseAt is a media query; ui.js closes the open sheet when it starts
	// to match, for example "(min-width: 768px)" for a mobile-only sheet
	// whose content the desktop layout shows inline.
	CloseAt string
	// Class is appended to the sheet panel classes.
	Class string
	// Attrs pass through to the <dialog> element.
	Attrs templ.Attributes
}

func sheetSide(s SheetSide) string {
	if s == SheetRight {
		return string(SheetRight)
	}
	return string(SheetLeft)
}

// sheetClass is the panel look. Position, size, and the slide live in
// input.css (dialog[data-ui-sheet]), because the UA <dialog> styles need
// overriding and Tailwind 3.4 has no @starting-style.
func sheetClass(s SheetSide) string {
	if s == SheetRight {
		return "border-l border-border bg-card p-0 text-left text-foreground shadow-2xl"
	}
	return "border-r border-border bg-card p-0 text-left text-foreground shadow-2xl"
}

func sheetAttrs(p SheetProps) templ.Attributes {
	a := templ.Attributes{
		"id":              p.ID,
		"data-ui-dialog":  "static",
		"data-ui-sheet":   sheetSide(p.Side),
		"aria-labelledby": DialogTitleID(p.ID),
		"class":           sheetClass(p.Side),
	}
	if p.Description != "" {
		a["aria-describedby"] = DialogDescriptionID(p.ID)
	}
	if p.CloseAt != "" {
		a["data-ui-sheet-close-at"] = p.CloseAt
	}
	return Attrs(a, templ.Attributes{"class": p.Class}, p.Attrs)
}

// OpenSheetAttrs are the attributes of the button that opens the Sheet
// with this id. ui.js keeps aria-expanded in sync while it is open.
func OpenSheetAttrs(id string) templ.Attributes {
	return templ.Attributes{
		"data-ui-dialog-open": id,
		"aria-haspopup":       "dialog",
		"aria-controls":       id,
		"aria-expanded":       "false",
	}
}
