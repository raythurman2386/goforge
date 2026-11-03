// Package pages holds the top-level page templates and their helpers.
package pages

import (
	"fmt"
	"math"

	"github.com/raythurman2386/goforge/internal/docker"
)

// PruneSummaryText is the Prune System toast (t1905u), shared by
// POST /dashboard/prune and POST /settings/prune. Bytes use formatBytes, the
// same units as the Docker Disk Usage card. When no space was freed it says
// "Nothing to free" rather than "Pruned 0 B".
func PruneSummaryText(s docker.PruneSummary) string {
	counts := fmt.Sprintf("%s, %s, %s",
		plural(s.Containers, "container", "containers"),
		plural(s.Images, "image", "images"),
		plural(s.BuildCacheRecords, "build cache record", "build cache records"))
	switch {
	case s.Empty():
		return "Nothing to free: no unused GoForge containers or images, and no unused build cache."
	case s.SpaceReclaimed == 0:
		return "Nothing to free. Removed " + counts + "."
	default:
		return "Pruned " + formatBytes(clampInt64(s.SpaceReclaimed)) + ": " + counts
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// clampInt64 converts a byte count for formatBytes; no engine frees 8 EiB.
func clampInt64(n uint64) int64 {
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}
