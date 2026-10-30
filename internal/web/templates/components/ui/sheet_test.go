package ui

import (
	"testing"

	"github.com/a-h/templ"
)

func TestSheetIsStaticDialogOnAnEdge(t *testing.T) {
	got := render(t, Sheet(SheetProps{ID: "nav", Title: "Menu", CloseAt: "(min-width: 768px)", Class: "md:hidden", Attrs: templ.Attributes{"data-testid": "s"}}), "<a href=\"/x\">x</a>")
	assertHas(t, got,
		`<dialog`, `id="nav"`, `data-ui-dialog="static"`, `data-ui-sheet="left"`, `aria-labelledby="nav-title"`,
		`data-ui-sheet-close-at="(min-width: 768px)"`, "md:hidden", `data-testid="s"`, "border-r",
		`<h2 id="nav-title"`, ">Menu</h2>", `aria-label="Close Menu"`, `data-ui-dialog-close`, `<a href="/x">x</a>`)
	assertLacks(t, got, `aria-describedby`, `data-ui-dialog="ephemeral"`, `open`)

	right := render(t, Sheet(SheetProps{ID: "f", Title: "Filters", Description: "Narrow it", Side: SheetRight}), "")
	assertHas(t, right, `data-ui-sheet="right"`, `aria-describedby="f-description"`, `id="f-description"`, "border-l")
	assertLacks(t, right, "data-ui-sheet-close-at")

	if sheetSide("diagonal") != "left" {
		t.Error("unknown side should default to left")
	}
}

func TestOpenSheetAttrs(t *testing.T) {
	a := OpenSheetAttrs("nav")
	want := map[string]string{"data-ui-dialog-open": "nav", "aria-haspopup": "dialog", "aria-controls": "nav", "aria-expanded": "false"}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s = %v, want %q", k, a[k], v)
		}
	}
}
