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
