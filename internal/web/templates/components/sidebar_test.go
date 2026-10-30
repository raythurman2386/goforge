package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
