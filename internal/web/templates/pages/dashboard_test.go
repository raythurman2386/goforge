package pages

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/models"
)

var cardRe = regexp.MustCompile(`class="(?:mt-4 )?card"`)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The loader must keep the HTMX swap contract and accessible status, and
// reserve one card per card the loaded stats render (4 resource + 1 memory +
// 4 disk) so the swap does not jump.
func TestDashboardStatsLoaderMirrorsStats(t *testing.T) {
	loader := render(t, DashboardStatsLoader())
	for _, want := range []string{
		`id="dashboard-stats"`,
		`hx-get="/dashboard/stats"`,
		`hx-trigger="load"`,
		`role="status"`,
		`aria-label="Loading stats"`,
	} {
		if !strings.Contains(loader, want) {
			t.Errorf("loader missing %s", want)
		}
	}

	stats := render(t, DashboardStats("tok", &models.SystemStats{DiskUsage: &models.DiskUsageInfo{}}, false))
	got, want := len(cardRe.FindAllString(loader, -1)), len(cardRe.FindAllString(stats, -1))
	if want != 9 {
		t.Fatalf("loaded stats render %d cards, expected 9; update the loader to match", want)
	}
	if got != want {
		t.Errorf("loader reserves %d cards, loaded stats render %d", got, want)
	}
}
