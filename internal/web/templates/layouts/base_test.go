package layouts

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
)

var inlineScriptRE = regexp.MustCompile(`<script(?:\s[^>]*)?>`)

// The mobile menu button opens the navigation Sheet through the kit dialog
// runtime; the old inline toggle script and overlay div are gone.
func TestAppLayoutMobileNavSheet(t *testing.T) {
	var buf bytes.Buffer
	if err := AppLayout("Dashboard", "tok").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`id="mobile-menu-btn"`, `data-ui-dialog-open="mobile-nav"`, `aria-controls="mobile-nav"`,
		`aria-haspopup="dialog"`, `aria-expanded="false"`, `aria-label="Open navigation menu"`,
		`<dialog`, `id="mobile-nav"`, `data-ui-sheet="left"`, `<aside id="sidebar"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("app layout missing %q", want)
		}
	}
	for _, legacy := range []string{"sidebar-overlay", "toggleSidebar", "-translate-x-full"} {
		if strings.Contains(html, legacy) {
			t.Errorf("app layout still has legacy %q", legacy)
		}
	}
	for _, tag := range inlineScriptRE.FindAllString(html, -1) {
		if !strings.Contains(tag, " src=") {
			t.Errorf("inline script left in the layout: %s", tag)
		}
	}
	// The sheet sits outside <main>, after the layout row.
	if strings.Index(html, `id="mobile-nav"`) < strings.Index(html, "</main>") {
		t.Error("mobile nav sheet renders inside <main>")
	}
}

// Every script Base loads carries a ?v= cache-bust like the stylesheet, so a
// new deploy never runs with a script the browser or CDN cached from an
// older one (a stale ui.js left the mobile nav Sheet unopenable).
func TestBaseScriptsCacheBusted(t *testing.T) {
	var buf bytes.Buffer
	if err := Base("Dashboard", "tok").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	srcs := regexp.MustCompile(`<script src="([^"]+)"`).FindAllStringSubmatch(html, -1)
	if len(srcs) != len(baseScripts) {
		t.Fatalf("got %d script tags, want %d", len(srcs), len(baseScripts))
	}
	for i, m := range srcs {
		want := "/static/js/" + baseScripts[i] + "?v="
		if !strings.HasPrefix(m[1], want) || len(m[1]) == len(want) {
			t.Errorf("script %d src = %q, want %s<version>", i, m[1], want)
		}
	}
}
