package pages_services //nolint:revive,stylecheck // matches the templ package name

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
)

// t1953u: the restore points card loads into a skeleton that copies the
// loaded list (heading, intro line, compact table, how-to, command), inside
// a busy status region the drive waits on, and keeps the load swap.
func TestRestorePointsLoaderMirrorsList(t *testing.T) {
	var buf bytes.Buffer
	if err := restorePointsCard("inst-1").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	loader := buf.String()
	for _, want := range []string{
		`id="service-restore-points"`,
		`hx-get="/services/inst-1/restore-points"`,
		`hx-trigger="load"`,
		`hx-swap="innerHTML"`,
		`role="status"`,
		`aria-label="Loading restore points"`,
		`aria-busy="true"`,
		`motion-safe:animate-pulse`,
		`<h3 class="text-lg font-bold text-foreground tracking-tight">Restore points</h3>`,
	} {
		if !strings.Contains(loader, want) {
			t.Errorf("loader missing %s", want)
		}
	}
	if strings.Contains(loader, "Loading restore points…") {
		t.Error("loader still shows the one-line text placeholder")
	}

	buf.Reset()
	v := &backup.InstanceRestorePointsView{InstanceID: "inst-1", Points: []backup.InstanceRestorePoint{{RunKey: "r1", StartedAt: time.Now()}, {RunKey: "r2", StartedAt: time.Now()}}}
	if err := RestorePoints(v).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	loaded := buf.String()
	// Same heading, a table header plus one skeleton row per loaded row (two).
	if !strings.Contains(loaded, `<h3 class="text-lg font-bold text-foreground tracking-tight">Restore points</h3>`) {
		t.Fatal("loaded heading changed; update restorePointsSkeleton")
	}
	if got, want := strings.Count(loader, `skeleton-line h-6 w-full`), 1+strings.Count(loaded, `data-testid="service-restore-point"`); got != want {
		t.Errorf("loader reserves %d table rows, a two-point list renders %d", got, want)
	}
}
