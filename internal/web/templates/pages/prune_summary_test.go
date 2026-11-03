package pages

import (
	"testing"

	"github.com/raythurman2386/goforge/internal/docker"
)

// t1905u: the toast names bytes in the disk card's units and every count,
// and zero bytes reads "Nothing to free", never "Pruned 0 B".
func TestPruneSummaryText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   docker.PruneSummary
		want string
	}{
		{
			name: "freed",
			in:   docker.PruneSummary{SpaceReclaimed: 3672645263, Containers: 2, Images: 5, BuildCacheRecords: 41},
			want: "Pruned 3.42 GB: 2 containers, 5 images, 41 build cache records",
		},
		{
			name: "singular",
			in:   docker.PruneSummary{SpaceReclaimed: 2048, Containers: 1, Images: 1, BuildCacheRecords: 1},
			want: "Pruned 2.00 KB: 1 container, 1 image, 1 build cache record",
		},
		{
			name: "nothing",
			in:   docker.PruneSummary{},
			want: "Nothing to free: no unused GoForge containers or images, and no unused build cache.",
		},
		{
			name: "removed but zero bytes",
			in:   docker.PruneSummary{Containers: 1},
			want: "Nothing to free. Removed 1 container, 0 images, 0 build cache records.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PruneSummaryText(tt.in); got != tt.want {
				t.Fatalf("PruneSummaryText(%+v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
