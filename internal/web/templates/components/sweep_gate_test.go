package components

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Path A PR7 grep gates (docs/reference/ui-kit.md): pages and shared
// components use components/ui, never the legacy class dialect, inline
// event handlers, inline scripts, or the old modal and menu ids.
// components/ui itself is excluded from the class gates because it owns
// those classes; the handler and script gates below cover it too.
func TestPagesUseKit(t *testing.T) {
	banned := []string{
		`confirm(`, `class="btn`, `class="card`, `class="input`,
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

// Inline event handlers in markup (onchange="...") or passed through
// templ.Attributes ("onchange": "..."), any on* name, not only onclick,
// plus htmx hx-on attributes, which also evaluate inline code.
var inlineHandlerRE = regexp.MustCompile(`\bon[a-z]+\s*=|"on[a-z]+"\s*:|\bhx-on[:-]`)

// A <script> tag without src. Behavior lives in static/js (app.js for page
// hooks, ui.js for the kit) behind delegated data-* listeners.
var scriptTagRE = regexp.MustCompile(`<script(?:\s[^>]*)?>`)

// inlineHandlerAllow lists "path: match" pairs that are not handlers. Empty
// today; add an entry only for a real false positive under components/ui.
var inlineHandlerAllow = map[string]bool{}

// TestNoInlineHandlersOrScripts walks every template source, components/ui
// included, so pages stay ready for a strict script-src CSP.
func TestNoInlineHandlersOrScripts(t *testing.T) {
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		isTempl := strings.HasSuffix(path, ".templ")
		isGo := strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, "_templ.go")
		if !isTempl && !isGo {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range inlineHandlerRE.FindAllString(string(b), -1) {
			if !inlineHandlerAllow[filepath.ToSlash(path)+": "+m] {
				t.Errorf("%s has inline handler %q; use a delegated data-* listener in static/js/app.js", path, m)
			}
		}
		if isTempl {
			for _, tag := range scriptTagRE.FindAllString(string(b), -1) {
				if !strings.Contains(tag, " src=") {
					t.Errorf("%s has inline script %q; move it to static/js", path, tag)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The gate regexes catch what they are for and leave look-alikes alone.
func TestInlineHandlerREMatches(t *testing.T) {
	for _, bad := range []string{`<select onchange="x()">`, `onclick="go()"`, `"onchange":   "toggle(this.value)",`, `oninput = "y"`, `hx-on::after-request="x"`, `"hx-on:click": "y"`} {
		if !inlineHandlerRE.MatchString(bad) {
			t.Errorf("gate misses %q", bad)
		}
	}
	for _, ok := range []string{`<button type="submit">`, `data-option="a"`, `hx-get="/x"`, `Description: "on"`, `"data-on": ""`} {
		if inlineHandlerRE.MatchString(ok) {
			t.Errorf("gate flags %q", ok)
		}
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
