package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Path A PR7 grep gates (docs/reference/ui-kit.md): pages and shared
// components use components/ui, never the legacy class dialect, inline
// click handlers, or the old modal and menu ids. components/ui itself is
// excluded because it owns those classes.
func TestPagesUseKit(t *testing.T) {
	banned := []string{
		`onclick=`, `confirm(`, `class="btn`, `class="card`, `class="input`,
		`class="select`, `class="label`, `id="modal"`, `id="menu-button"`,
	}
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && filepath.Base(path) == "ui" {
			return filepath.SkipDir
		}
		if info.IsDir() || !strings.HasSuffix(path, ".templ") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range banned {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %q; use components/ui", path, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestToastDismissIsDelegated(t *testing.T) {
	got := renderComp(t, Toast("Saved", "success"))
	for _, want := range []string{`data-toast`, `data-toast-dismiss`, `aria-label="Dismiss"`, `h-11 w-11`, `md:h-8 md:w-8`} {
		if !strings.Contains(got, want) {
			t.Errorf("toast missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "onclick") {
		t.Errorf("toast still has an inline handler: %s", got)
	}
}
