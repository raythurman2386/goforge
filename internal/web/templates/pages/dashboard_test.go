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

// ui.Card renders "card" first, then any caller class.
var cardRe = regexp.MustCompile(`class="card(?: mt-4)?"`)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// The loader must keep the HTMX swap contract and accessible busy status, and
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
		`aria-busy="true"`,
		`motion-safe:animate-pulse`,
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

// t1695u: the recent agent actions loader keeps the swap contract and
// reserves the card's Flush shell (header, then body).
func TestRecentAgentActionsLoaderMirrorsCard(t *testing.T) {
	loader := render(t, RecentAgentActionsLoader())
	for _, want := range []string{
		`id="recent-agent-actions-loader"`,
		`hx-get="/dashboard/activity"`,
		`hx-swap="outerHTML"`,
		`aria-busy="true"`,
		`class="card p-0 mt-8 overflow-hidden"`,
		`class="card-header"`,
		`class="card-body"`,
	} {
		if !strings.Contains(loader, want) {
			t.Errorf("loader missing %s", want)
		}
	}
}

// t1876u: Prune System enables when the only reclaimable space is build cache
// (folded into ReclaimableSize), the alert names the build cache share, and
// the confirm copy says build cache is engine-wide.
func TestDashboardStatsPruneBuildCacheOnly(t *testing.T) {
	du := &models.DiskUsageInfo{BuildCacheSize: 3 << 30, BuildCacheReclaimable: 2 << 30, ReclaimableSize: 2 << 30}
	out := render(t, DashboardStats("tok", &models.SystemStats{DiskUsage: du}, true))
	if strings.Contains(out, "Nothing to prune") {
		t.Fatal("prune button disabled although build cache is reclaimable")
	}
	for _, want := range []string{
		`action="/dashboard/prune"`,
		"unused Docker build cache on this host",
		"engine-wide",
		"can be freed by pruning",
		"of unused build cache",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats missing %q", want)
		}
	}

	none := render(t, DashboardStats("tok", &models.SystemStats{DiskUsage: &models.DiskUsageInfo{BuildCacheSize: 1 << 30}}, true))
	if !strings.Contains(none, "Nothing to prune") {
		t.Fatal("prune button enabled with nothing reclaimable")
	}
	if strings.Contains(none, "of unused build cache") {
		t.Fatal("build cache note shown with nothing reclaimable")
	}
}
