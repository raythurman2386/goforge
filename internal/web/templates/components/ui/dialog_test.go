package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

var dialogIDRe = regexp.MustCompile(`<dialog [^>]*\bid="(dialog-[0-9a-f]{8})"`)

func TestDialog_ARIAWiring(t *testing.T) {
	got := render(t, Dialog(DialogProps{ID: "d1", Title: "Add Git Source", Description: "Connect a provider."}), "<p>body</p>")
	assertHas(t, got,
		`<dialog `, `id="d1"`,
		`data-ui-dialog="ephemeral"`,
		`aria-labelledby="d1-title"`,
		`aria-describedby="d1-description"`,
		`<h2 id="d1-title"`, ">Add Git Source</h2>",
		`<p id="d1-description"`,
		`id="d1-error" data-ui-dialog-error role="alert"`,
		`aria-label="Close dialog"`, `data-ui-dialog-close`, `type="button"`,
		`<p>body</p>`,
	)
	// No legacy modal contract, no role=dialog div (native <dialog> carries it).
	assertLacks(t, got, `id="modal"`, `data-modal`, `role="dialog"`, `aria-modal`, `data-ui-dialog-nondismissible`)
	// The close button's icon is hidden; the label names it.
	assertHas(t, got, `aria-hidden="true"`)
}

func TestDialog_NoDescriptionNoDescribedBy(t *testing.T) {
	got := render(t, Dialog(DialogProps{ID: "d2", Title: "T"}), "")
	assertLacks(t, got, "aria-describedby", `id="d2-description"`)
}

func TestDialog_GeneratedIDsAreUniqueAndConsistent(t *testing.T) {
	a := render(t, Dialog(DialogProps{Title: "A"}), "")
	b := render(t, Dialog(DialogProps{Title: "B"}), "")
	ma, mb := dialogIDRe.FindStringSubmatch(a), dialogIDRe.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		t.Fatalf("generated id missing:\n%s\n%s", a, b)
	}
	if ma[1] == mb[1] {
		t.Fatalf("two dialogs share id %q, so they cannot stack", ma[1])
	}
	id := ma[1]
	assertHas(t, a, `aria-labelledby="`+id+`-title"`, `<h2 id="`+id+`-title"`, `id="`+id+`-error"`)
}

func TestDialog_NonDismissibleStaticAndPassthrough(t *testing.T) {
	got := render(t, Dialog(DialogProps{ID: "tok", Title: "API Token Created", NonDismissible: true, Class: "max-h-screen", Attrs: templ.Attributes{"data-testid": "x"}}), "")
	assertHas(t, got, `data-ui-dialog-nondismissible`, `data-testid="x"`, `max-h-screen`, `data-ui-dialog="ephemeral"`)
	static := render(t, Dialog(DialogProps{ID: "s", Title: "S", Static: true}), "")
	assertHas(t, static, `data-ui-dialog="static"`)
}

func TestConfirmDialog(t *testing.T) {
	got := render(t, ConfirmDialog(), "")
	assertHas(t, got,
		`<dialog id="ui-confirm"`, `data-ui-dialog="static"`, `data-ui-confirm`,
		`role="alertdialog"`,
		`aria-labelledby="ui-confirm-title"`, `aria-describedby="ui-confirm-description"`,
		`<h2 id="ui-confirm-title"`, `id="ui-confirm-description" data-ui-confirm-message`,
		`data-ui-confirm-cancel`, `data-ui-confirm-accept`, `autofocus`,
	)
	// Cancel comes first so it takes focus, and both are plain buttons.
	if strings.Index(got, "data-ui-confirm-cancel") > strings.Index(got, "data-ui-confirm-accept") {
		t.Error("Cancel must come before Confirm")
	}
	if strings.Count(got, `type="button"`) != 2 {
		t.Errorf("want 2 type=button, got:\n%s", got)
	}
}

func TestOpenDialogAndConfirmAttrs(t *testing.T) {
	a := OpenDialogAttrs("/settings/git/new")
	for k, want := range map[string]string{"hx-get": "/settings/git/new", "hx-target": "body", "hx-swap": "beforeend", "aria-haspopup": "dialog"} {
		if a[k] != want {
			t.Errorf("OpenDialogAttrs[%s] = %v, want %q", k, a[k], want)
		}
	}
	c := ConfirmAttrs("Delete?", ConfirmDanger, "Delete")
	if c["hx-confirm"] != "Delete?" || c["data-ui-confirm-tone"] != "danger" || c["data-ui-confirm-label"] != "Delete" {
		t.Errorf("ConfirmAttrs = %v", c)
	}
	p := ConfirmAttrs("Go?", ConfirmPrimary, "")
	if len(p) != 1 {
		t.Errorf("primary, no label: want only hx-confirm, got %v", Keys(p))
	}
}
