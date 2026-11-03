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

// t1905u: the enabled Prune button is the htmx requester. It posts to
// /dashboard/prune, swaps #dashboard-stats, disables itself and shows a
// spinner and "Pruning…" while it runs, and keeps the kit confirm (text and
// danger tone). The form keeps its plain POST for no-JS. The disabled
// "Nothing to prune" state is unchanged and makes no request.
func TestDashboardStatsPruneButtonFeedback(t *testing.T) {
	du := &models.DiskUsageInfo{ImagesSize: 1 << 30, ReclaimableSize: 1 << 30}
	out := render(t, DashboardStats("tok", &models.SystemStats{DiskUsage: du}, true))
	start := strings.Index(out, `data-testid="dashboard-prune"`)
	if start < 0 {
		t.Fatal("enabled prune button missing")
	}
	btnStart := strings.LastIndex(out[:start], "<button")
	btnEnd := strings.Index(out[start:], "</button>") + start
	btn := out[btnStart:btnEnd]
	for _, want := range []string{
		`type="submit"`,
		`hx-post="/dashboard/prune"`,
		`hx-target="#dashboard-stats"`,
		`hx-swap="outerHTML"`,
		`hx-disable="this"`,
		`hx-indicator="this"`,
		`hx-confirm="This removes stopped GoForge containers, unused GoForge images, and all unused Docker build cache on this host (build cache is engine-wide, not only GoForge builds). Continue?"`,
		`data-ui-confirm-tone="danger"`,
		`data-ui-confirm-label="Prune"`,
		`class="htmx-indicator animate-spin`,
		`group-[.htmx-request]:hidden">Prune System</span>`,
		`hidden group-[.htmx-request]:inline">Pruning…</span>`,
	} {
		if !strings.Contains(btn, want) {
			t.Errorf("enabled prune button missing %s\n%s", want, btn)
		}
	}
	for _, want := range []string{`method="POST" action="/dashboard/prune"`, `name="csrf_token" value="tok"`} {
		if !strings.Contains(out, want) {
			t.Errorf("prune form missing %s", want)
		}
	}

	none := render(t, DashboardStats("tok", &models.SystemStats{DiskUsage: &models.DiskUsageInfo{}}, true))
	for _, want := range []string{`disabled`, `title="Nothing to prune"`, `opacity-50 cursor-not-allowed`} {
		if !strings.Contains(none, want) {
			t.Errorf("disabled prune button missing %s", want)
		}
	}
	for _, bad := range []string{`hx-post="/dashboard/prune"`, "Pruning…", "htmx-indicator", "hx-confirm"} {
		if strings.Contains(none, bad) {
			t.Errorf("disabled prune button should not carry %s", bad)
		}
	}
}
