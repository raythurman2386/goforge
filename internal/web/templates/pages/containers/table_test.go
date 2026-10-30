package containers

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The containers list is a kit Table: the polled-on-event tbody keeps its
// id and hx-* attributes, the loading and empty rows are TableEmpty rows,
// and the legacy .table-th / .table-td classes are gone.
func TestContainersListTable(t *testing.T) {
	var buf bytes.Buffer
	if err := List("tok").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		`role="region"`, `tabindex="0"`, `aria-labelledby="container-list-caption"`,
		`<caption id="container-list-caption" class="sr-only">Containers</caption>`,
		`id="container-list-body"`, `hx-get="/containers/api"`, `hx-trigger="load, containerStopped from:body, containerRestarted from:body"`,
		`<tr data-ui-table-empty>`, `colspan="5"`, "table-fixed", "<colgroup>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("containers list missing %q", want)
		}
	}
	if n := strings.Count(got, `scope="col"`); n != 5 {
		t.Errorf("scope=col headers = %d, want 5", n)
	}
	if strings.Contains(got, "table-th") || strings.Contains(got, "table-td") {
		t.Error("legacy table-th/table-td classes still used")
	}

	buf.Reset()
	if err := ListAPI(nil, nil).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if empty := buf.String(); !strings.Contains(empty, `<tr data-ui-table-empty>`) || !strings.Contains(empty, "No containers yet") {
		t.Errorf("empty rows fragment = %s", empty)
	}
}
