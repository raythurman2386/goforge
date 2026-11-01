package ui

import (
	"strconv"

	"github.com/a-h/templ"
)

// TableProps configures Table. A table always has a caption (its
// accessible name) and sits in a horizontal scroll region that a keyboard
// user can focus (tabindex="0", role="region", labeled by the caption), so
// a wide table on a small screen scrolls with the arrow keys.
//
//	@ui.Table(ui.TableProps{Caption: "Containers"}) {
//		@ui.THead(nil) {
//			@ui.TR(nil) {
//				@ui.TH(ui.CellProps{}) { Name }
//			}
//		}
//		@ui.TBody(nil) { ... }
//	}
type TableProps struct {
	// ID is the <table> id. The caption id derives from it. Leave it empty
	// to get a generated id.
	ID string
	// Caption names the table. Required.
	Caption string
	// CaptionVisible shows the caption above the table; by default it is
	// sr-only (the page already has a visible heading for the table).
	CaptionVisible bool
	// Compact is the dense look (text-xs, py-1 cells) for small tables
	// inside a card section, such as Restore points.
	Compact bool
	// Fixed sets table-layout: fixed, for tables with a <colgroup>.
	Fixed bool
	// Class is appended to the <table> classes; WrapperClass to the scroll
	// region's.
	Class        string
	WrapperClass string
	// Attrs pass through to the <table> element.
	Attrs templ.Attributes
}

// CellAlign is the text alignment of a TH or TD.
type CellAlign string

// Cell alignments.
const (
	CellStart CellAlign = ""
	CellEnd   CellAlign = "end"
)

// CellProps configures TH and TD.
type CellProps struct {
	// Scope is the TH scope: "col" (default) or "row". Ignored on TD.
	Scope string
	// Colspan spans columns when greater than 1.
	Colspan int
	// Compact matches TableProps.Compact.
	Compact bool
	// Align right-aligns the cell (actions columns).
	Align CellAlign
	// Class is appended to the cell classes.
	Class string
	// Attrs pass through to the cell.
	Attrs templ.Attributes
}

// TableCaptionID is the id of a table's caption.
func TableCaptionID(tableID string) string { return tableID + "-caption" }

func resolveTable(p TableProps) TableProps {
	if p.ID == "" {
		p.ID = newID("table")
	}
	return p
}

// tableWrapClass is the focusable horizontal scroll region.
func tableWrapClass() string {
	return "overflow-x-auto rounded-ui ui-focus-ring"
}

func tableClass(p TableProps) string {
	switch {
	case p.Compact && p.Fixed:
		return "min-w-full table-fixed text-xs"
	case p.Compact:
		return "min-w-full text-xs"
	case p.Fixed:
		return "min-w-full table-fixed divide-y divide-divider"
	default:
		return "min-w-full divide-y divide-divider"
	}
}

func tableCaptionClass(visible bool) string {
	if visible {
		return "caption-top pb-2 text-left text-sm font-medium text-foreground"
	}
	return "sr-only"
}

func theadClass() string {
	return "bg-muted/60"
}

func tbodyClass() string {
	return "divide-y divide-divider bg-card"
}

func thClass(compact bool, align CellAlign) string {
	switch {
	case compact && align == CellEnd:
		return "py-1 pr-3 text-right font-medium text-foreground-secondary"
	case compact:
		return "py-1 pr-3 text-left font-medium text-foreground-secondary"
	case align == CellEnd:
		return "px-4 py-3 text-right text-xs font-semibold uppercase tracking-wide text-foreground-secondary"
	default:
		return "px-4 py-3 text-left text-xs font-semibold uppercase tracking-wide text-foreground-secondary"
	}
}

func tdClass(compact bool, align CellAlign) string {
	switch {
	case compact && align == CellEnd:
		return "py-1 pr-3 text-right"
	case compact:
		return "py-1 pr-3"
	case align == CellEnd:
		return "px-4 py-3 text-right text-sm"
	default:
		return "px-4 py-3 text-sm"
	}
}

func scopeOr(s string) string {
	if s == "row" || s == "rowgroup" || s == "colgroup" {
		return s
	}
	return "col"
}

func wrapAttrs(p TableProps) templ.Attributes {
	return Attrs(templ.Attributes{
		"class":           tableWrapClass(),
		"role":            "region",
		"tabindex":        "0",
		"aria-labelledby": TableCaptionID(p.ID),
		"data-ui-table":   true,
	}, templ.Attributes{"class": p.WrapperClass})
}

func tableAttrs(p TableProps) templ.Attributes {
	return Attrs(templ.Attributes{"id": p.ID, "class": tableClass(p)}, templ.Attributes{"class": p.Class}, p.Attrs)
}

func thAttrs(p CellProps) templ.Attributes {
	a := templ.Attributes{"scope": scopeOr(p.Scope), "class": thClass(p.Compact, p.Align)}
	if p.Colspan > 1 {
		a["colspan"] = strconv.Itoa(p.Colspan)
	}
	return Attrs(a, templ.Attributes{"class": p.Class}, p.Attrs)
}

func tdAttrs(p CellProps) templ.Attributes {
	a := templ.Attributes{"class": tdClass(p.Compact, p.Align)}
	if p.Colspan > 1 {
		a["colspan"] = strconv.Itoa(p.Colspan)
	}
	return Attrs(a, templ.Attributes{"class": p.Class}, p.Attrs)
}

func tableEmptyAttrs(colspan int) templ.Attributes {
	a := templ.Attributes{}
	if colspan > 1 {
		a["colspan"] = strconv.Itoa(colspan)
	}
	return a
}
