package components

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

// On md+ the sidebar must be viewport-tall and sticky rather than stretching
// with the page, so Log out stays visible after tall content (dashboard stats)
// swaps in.
func TestSidebarViewportLocked(t *testing.T) {
	var buf bytes.Buffer
	if err := Sidebar("tok").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"md:sticky", "md:h-screen", "Log out"} {
		if !strings.Contains(html, want) {
			t.Errorf("sidebar missing %q", want)
		}
	}
	if strings.Contains(html, "md:h-auto") {
		t.Error("sidebar uses md:h-auto, which stretches it with the page and pushes Log out off-screen")
	}
}

// The footer names the signed-in user with a decorative initials Avatar (the
// name is visible text next to it) and no remote image.
func TestSidebarUserFooter(t *testing.T) {
	render := func(ctx context.Context) string {
		var buf bytes.Buffer
		if err := Sidebar("tok").Render(ctx, &buf); err != nil { //nolint:contextcheck // templ reads the user from ctx at Render
			t.Fatal(err)
		}
		return buf.String()
	}
	if html := render(context.Background()); strings.Contains(html, "data-sidebar-user") {
		t.Error("sidebar shows a user footer with no user in context")
	}

	local := &models.User{Email: "ray.thurman@example.com"}
	html := render(context.WithValue(context.Background(), auth.UserContextKey, local))
	for _, want := range []string{"data-sidebar-user", `<span aria-hidden="true" class="inline-flex shrink-0 select-none`, ">RT</span>", "ray.thurman@example.com"} {
		if !strings.Contains(html, want) {
			t.Errorf("sidebar footer missing %q", want)
		}
	}
	if strings.Contains(html, "<img") {
		t.Error("sidebar avatar must not load a remote image")
	}

	gh := &models.User{Email: "r@example.com", GitHubUsername: sql.NullString{String: "raythurman2386", Valid: true}}
	html = render(context.WithValue(context.Background(), auth.UserContextKey, gh))
	for _, want := range []string{">raythurman2386</p>", ">r@example.com</p>"} {
		if !strings.Contains(html, want) {
			t.Errorf("GitHub user footer missing %q", want)
		}
	}
}

// The navigation renders twice from one templ: the static desktop aside
// (hidden below md) and the mobile Sheet. Both carry every data-nav link
// and Log out, and only the aside has id="sidebar".
func TestSidebarAndMobileNavShareNav(t *testing.T) {
	var a, s bytes.Buffer
	if err := Sidebar("tok").Render(context.Background(), &a); err != nil {
		t.Fatal(err)
	}
	if err := MobileNav("tok").Render(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
	aside, sheet := a.String(), s.String()
	for _, want := range []string{`<aside id="sidebar"`, "hidden", "md:flex"} {
		if !strings.Contains(aside, want) {
			t.Errorf("aside missing %q", want)
		}
	}
	if strings.Contains(aside, "-translate-x-full") {
		t.Error("aside still carries the legacy slide-in transform")
	}
	for _, want := range []string{
		`<dialog`, `id="` + MobileNavID + `"`, `data-ui-dialog="static"`, `data-ui-sheet="left"`,
		`data-ui-sheet-close-at="(min-width: 768px)"`, `aria-labelledby="mobile-nav-title"`, "Log out",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("mobile nav sheet missing %q", want)
		}
	}
	if strings.Contains(sheet, `id="sidebar"`) {
		t.Error("mobile nav duplicates id=sidebar")
	}
	if na, ns := strings.Count(aside, "data-nav="), strings.Count(sheet, "data-nav="); na != 7 || ns != na {
		t.Errorf("data-nav links: aside=%d sheet=%d, want 7 each (Activity included)", na, ns)
	}
}
