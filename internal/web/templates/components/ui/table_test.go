package ui

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestTableScrollRegionAndCaption(t *testing.T) {
	got := render(t, Table(TableProps{ID: "t1", Caption: "Containers", Fixed: true, Attrs: templ.Attributes{"data-testid": "x"}}), "<tbody></tbody>")
	assertHas(t, got,
		`role="region"`, `tabindex="0"`, `aria-labelledby="t1-caption"`, `data-ui-table`, "overflow-x-auto", "ui-focus-ring",
		`<table`, `id="t1"`, "table-fixed", `data-testid="x"`,
		`<caption id="t1-caption" class="sr-only">Containers</caption>`)

	visible := render(t, Table(TableProps{ID: "t2", Caption: "Shown", CaptionVisible: true, Compact: true, WrapperClass: "mt-2"}), "")
	assertHas(t, visible, "caption-top", "text-xs", "mt-2")
	assertLacks(t, visible, `class="sr-only"`)

	gen := render(t, Table(TableProps{Caption: "Generated"}), "")
	if !strings.Contains(gen, `id="table-`) || !strings.Contains(gen, `-caption"`) {
		t.Errorf("generated table id missing:\n%s", gen)
	}
}

func TestTableCells(t *testing.T) {
	th := render(t, TH(CellProps{}), "Name")
	assertHas(t, th, `<th class=`, `scope="col"`, "text-left", "uppercase")

	row := render(t, TH(CellProps{Scope: "row"}), "web")
	assertHas(t, row, `scope="row"`)

	bad := render(t, TH(CellProps{Scope: "nonsense"}), "x")
	assertHas(t, bad, `scope="col"`)

	end := render(t, TH(CellProps{Align: CellEnd, Class: "pr-6"}), "")
	assertHas(t, end, "text-right", "pr-6")

	td := render(t, TD(CellProps{Colspan: 4, Compact: true, Class: "font-mono"}), "x")
	assertHas(t, td, `<td`, `colspan="4"`, "py-1", "font-mono")
	assertLacks(t, td, "scope=")

	one := render(t, TD(CellProps{Colspan: 1}), "x")
	assertLacks(t, one, "colspan")

	empty := render(t, TableEmpty(5), "Nothing here")
	assertHas(t, empty, `<tr data-ui-table-empty>`, `colspan="5"`, "Nothing here")

	tr := render(t, TR(templ.Attributes{"class": "hover:bg-muted/60", "data-testid": "r"}), "")
	assertHas(t, tr, `<tr`, "hover:bg-muted/60", `data-testid="r"`)

	body := render(t, TBody(templ.Attributes{"id": "b", "hx-get": "/x"}), "")
	assertHas(t, body, `<tbody`, `id="b"`, `hx-get="/x"`, "divide-y")

	head := render(t, THead(nil), "")
	assertHas(t, head, `<thead`)
}
