package components

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/models"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

const (
	statsGridClass = `class="grid grid-cols-1 gap-5 sm:grid-cols-2"`
	statsTileClass = `class="rounded-lg border border-divider bg-muted/50 p-4"`
)

var (
	statsLineRe    = regexp.MustCompile(`<(dt|dd) class="([^"]*)"`)
	skeletonLineRe = regexp.MustCompile(`class="motion-safe:animate-pulse bg-highlight skeleton-line ([^"]*)"`)
	marginRe       = regexp.MustCompile(`\bmt-\S+`)
	heightRe       = regexp.MustCompile(`\bh-\S+`)
)

// lineHeight is the Tailwind line height of a text size, as the h-* class a
// skeleton line needs to take the same space.
var lineHeight = map[string]string{"text-xs": "h-4", "text-sm": "h-5", "text-lg": "h-7", "text-3xl": "h-9"}

// tiles splits html at each stats tile.
func tiles(html string) []string {
	parts := strings.Split(html, statsTileClass)
	return parts[1:]
}

// statsLines is the margin and height of each dt and dd in a Stats tile.
func statsLines(t *testing.T, tile string) []string {
	t.Helper()
	ms := statsLineRe.FindAllStringSubmatch(tile, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		cls := m[2]
		h := heightRe.FindString(cls) // the bars set h-2 on the dd
		for size, lh := range lineHeight {
			if regexp.MustCompile(`\b` + size + `\b`).MatchString(cls) {
				h = lh
			}
		}
		if h == "" {
			t.Fatalf("stats line %q has no text size or height", cls)
		}
		out = append(out, strings.TrimSpace(marginRe.FindString(cls)+" "+h))
	}
	return out
}

func skeletonLines(tile string) []string {
	ms := skeletonLineRe.FindAllStringSubmatch(tile, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, strings.TrimSpace(marginRe.FindString(m[1])+" "+heightRe.FindString(m[1])))
	}
	return out
}

// t1953u: the skeleton that fills [data-metrics-frame] copies Stats tile by
// tile, so the first frame does not move the cards below it.
func TestStatsSkeletonMirrorsStats(t *testing.T) {
	skel := renderComponent(t, StatsSkeleton())
	for _, want := range []string{`role="status"`, `aria-label="Loading resource usage"`, `aria-busy="true"`, `motion-safe:animate-pulse`, statsGridClass} {
		if !strings.Contains(skel, want) {
			t.Errorf("skeleton missing %s", want)
		}
	}
	stats := renderComponent(t, Stats(&models.ContainerMetrics{}))
	if !strings.Contains(stats, `<dl `+statsGridClass) {
		t.Fatal("Stats grid classes changed; update statsSkeletonGrid to match")
	}
	st, sk := tiles(stats), tiles(skel)
	if len(st) != 4 || len(sk) != len(st) {
		t.Fatalf("Stats renders %d tiles, skeleton %d; want 4 each", len(st), len(sk))
	}
	for i := range st {
		want, got := statsLines(t, st[i]), skeletonLines(sk[i])
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("tile %d: skeleton lines %v, Stats lines %v", i, got, want)
		}
	}
}

// t1953u D2: the unavailable frame reserves the same grid as Stats, so
// unavailable to running and back keeps the card height.
func TestStatsUnavailableReservesStatsGrid(t *testing.T) {
	html := renderComponent(t, StatsUnavailable("Metrics are not available: the container is not running."))
	if !strings.Contains(html, statsGridClass) || len(tiles(html)) != 4 {
		t.Fatalf("unavailable frame does not reserve the Stats grid:\n%s", html)
	}
	if !strings.Contains(html, `class="invisible" aria-hidden="true"`) || strings.Contains(html, `aria-busy`) {
		t.Fatal("reserved grid must be invisible and hidden, and the frame not busy")
	}
}

// t1953u: the stream opens on the skeleton, not the old spinner.
func TestResourceUsageCardFrameHoldsSkeleton(t *testing.T) {
	html := renderComponent(t, ResourceUsageCard("abc123", ""))
	if !strings.Contains(html, `<div data-metrics-frame><div role="status" aria-label="Loading resource usage" aria-busy="true" data-stats-skeleton>`) {
		t.Fatalf("frame does not open on the bare StatsSkeleton:\n%s", html)
	}
	if strings.Contains(html, "Connecting to metrics stream") || strings.Contains(html, "animate-spin") {
		t.Fatal("frame still holds the spinner")
	}
}
