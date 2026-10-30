package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestStatsShowsNetworkAndBlockIO(t *testing.T) {
	var buf bytes.Buffer
	m := &models.ContainerMetrics{CPUPercent: 12.5, MemoryUsage: 64 << 20, MemoryLimit: 256 << 20, MemoryPercent: 25,
		NetworkRx: 3 << 20, NetworkTx: 1536, BlockRead: 10 << 30, BlockWrite: 0}
	if err := Stats(m).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"12.50%", "Network I/O", "3.0 MiB in / 1.5 KiB out", "Block I/O", "10.0 GiB read / 0 B written"} {
		if !strings.Contains(got, want) {
			t.Errorf("stats lacks %q:\n%s", want, got)
		}
	}
}

func TestStatsUnavailableShowsReason(t *testing.T) {
	var buf bytes.Buffer
	if err := StatsUnavailable("Metrics are not available: the container is not running.").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "the container is not running") || !strings.Contains(got, "data-stats-unavailable") {
		t.Fatalf("unavailable frame:\n%s", got)
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536 << 10: "1.5 MiB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
