package monitor

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"

	"github.com/raythurman2386/goforge/internal/docker"
)

func TestCalculateDiskUsage_ManagedOnly(t *testing.T) {
	t.Parallel()

	managed := map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue}
	usage := &types.DiskUsage{
		Images: []*image.Summary{
			{Size: 10, Containers: 0, Labels: managed},
			{Size: 99, Containers: 0, Labels: map[string]string{"app": "other"}},
		},
		Containers: []*types.Container{
			{SizeRw: 5, State: "running", Labels: managed},
			{SizeRw: 4, State: "exited", Labels: managed},
			{SizeRw: 50, State: "exited"},
		},
		Volumes: []*volume.Volume{
			{Name: "goforge-vol-db", UsageData: &volume.UsageData{Size: 7}},
			{Name: "other-data", UsageData: &volume.UsageData{Size: 70}},
		},
		BuildCache: []*types.BuildCache{{Size: 3}},
	}

	info := (&Collector{}).calculateDiskUsage(usage)
	if info.ImagesSize != 10 {
		t.Fatalf("images = %d, want 10", info.ImagesSize)
	}
	if info.ContainersSize != 9 {
		t.Fatalf("containers = %d, want 9", info.ContainersSize)
	}
	if info.VolumesSize != 7 {
		t.Fatalf("volumes = %d, want 7", info.VolumesSize)
	}
	if info.BuildCacheSize != 3 {
		t.Fatalf("build cache = %d, want 3", info.BuildCacheSize)
	}
	// Unused managed image plus the stopped managed container.
	if info.ReclaimableSize != 14 {
		t.Fatalf("reclaimable = %d, want 14", info.ReclaimableSize)
	}
}
