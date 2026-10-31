package devui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGalleryRenders(t *testing.T) {
	var buf bytes.Buffer
	if err := Gallery("tok").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"<h1", "UI kit gallery", `<main id="main"`,
		`aria-labelledby="tokens-title"`, "--gf-background", "bg-primary-500",
		`class="btn-primary"`, `aria-label="Refresh"`,
		`aria-describedby="gallery-name-help"`, `class="sr-only"`,
		`role="switch"`, `aria-invalid="true"`, `<fieldset`, `<legend class="label">`, `<textarea`, `id="checkboxes"`,
		`aria-labelledby="navtabs-title"`, `aria-label="Gallery sections"`, `data-ui-navtabs`,
		`aria-labelledby="tabs-title"`, `data-ui="tabs"`, `role="tablist"`, `aria-label="Log source"`,
		`id="gallery-tabs-tab-build"`, `aria-controls="gallery-tabs-panel-runtime"`, `role="tabpanel"`,
		`aria-labelledby="tables-title"`, `id="gallery-table"`, `role="region"`, `aria-labelledby="gallery-table-caption"`,
		`scope="row"`, `data-ui-table-empty`, `id="gallery-card-table"`,
		`aria-labelledby="sheets-title"`, `id="gallery-sheet-left"`, `data-ui-sheet="left"`, `data-ui-sheet="right"`,
		`data-ui-dialog-open="gallery-sheet-left"`, `aria-controls="gallery-sheet-right"`,
		`src="/static/js/ui.js?v=`,
		`aria-labelledby="badges-title"`, `aria-labelledby="alerts-title"`,
		`aria-labelledby="skeletons-title"`, `aria-labelledby="avatars-title"`,
		`aria-labelledby="breadcrumbs-title"`, `aria-label="Breadcrumb"`,
		`aria-current="page"`, `data-ui="alert"`, `aria-label="Dismiss"`,
		`role="alert"`, `aria-busy="true"`, `aria-disabled="true"`,
		`motion-safe:animate-pulse`,
		`aria-labelledby="dialogs-title"`, `id="gallery-dialog-open"`, `aria-haspopup="dialog"`,
		`hx-get="/dev/ui/dialog?depth=1"`, `data-ui-dialog-open="gallery-static-dialog"`,
		`id="gallery-static-dialog"`, `data-ui-dialog="static"`,
		`<dialog `, `id="ui-confirm"`, `role="alertdialog"`,
		`id="gallery-confirm"`, `data-ui-confirm-tone="danger"`, `id="gallery-confirm-form"`,
		`aria-labelledby="overlays-title"`,
		`id="gallery-menu-trigger"`, `aria-haspopup="menu"`, `popovertarget="gallery-menu"`, `role="menu"`, `role="menuitem"`, `role="separator"`,
		`id="gallery-menu-icon-trigger"`, `aria-label="More actions"`, `aria-describedby="gallery-tip-more"`,
		`id="gallery-popover-trigger"`, `id="gallery-popover"`, `popover="auto"`, `aria-labelledby="gallery-popover-title"`,
		`role="tooltip"`, `popover="manual"`, `id="gallery-tip-restart"`, `aria-label="Restart web"`, `aria-describedby="gallery-tip-image"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("gallery missing %q", want)
		}
	}
}

func TestSwatchClassCoversEveryToken(t *testing.T) {
	for _, name := range colorTokens {
		if swatchClass(name) == "" {
			t.Errorf("swatchClass(%q) is empty", name)
		}
	}
	for _, step := range primarySteps {
		if primarySwatchClass(step) == "" {
			t.Errorf("primarySwatchClass(%q) is empty", step)
		}
	}
}

func TestDemoDialog(t *testing.T) {
	var buf bytes.Buffer
	if err := DemoDialog(2, true).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		`data-ui-dialog="ephemeral"`, `data-ui-dialog-nondismissible`, `data-depth="2"`,
		`id="demo-dialog-name-2"`, `<label`, `data-testid="demo-stack"`, `data-testid="demo-fail"`,
		`data-testid="demo-save"`, `data-ui-dialog-close`, `role="alert"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("demo dialog missing %q", want)
		}
	}
}
