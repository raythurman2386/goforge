package monitor

import (
	"testing"
	"time"

	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"

	"github.com/raythurman2386/goforge/internal/docker"
)

func TestCalculateDiskUsage_ManagedOnly(t *testing.T) {
	t.Parallel()

	managed := map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue}
	usage := &client.DiskUsageResult{
		Images: client.ImagesDiskUsage{Items: []image.Summary{
			{Size: 10, Containers: 0, Labels: managed},
			{Size: 99, Containers: 0, Labels: map[string]string{"app": "other"}},
		}},
		Containers: client.ContainersDiskUsage{Items: []container.Summary{
			{SizeRw: 5, State: "running", Labels: managed},
			{SizeRw: 4, State: "exited", Labels: managed},
			{SizeRw: 50, State: "exited"},
		}},
		Volumes: client.VolumesDiskUsage{Items: []volume.Volume{
			{Name: "goforge-vol-db", UsageData: &volume.UsageData{Size: 7}},
			{Name: "other-data", UsageData: &volume.UsageData{Size: 70}},
		}},
		BuildCache: client.BuildCacheDiskUsage{Items: []build.CacheRecord{{Size: 3}}},
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
	if info.BuildCacheReclaimable != 3 {
		t.Fatalf("build cache reclaimable = %d, want 3", info.BuildCacheReclaimable)
	}
	// Unused managed image, the stopped managed container, and unused build cache.
	if info.ReclaimableSize != 17 {
		t.Fatalf("reclaimable = %d, want 17", info.ReclaimableSize)
	}
}

// t1876u: build cache that is neither in use nor shared is reclaimable and is
// folded into ReclaimableSize, so a host whose only waste is build cache still
// enables Prune System.
func TestCalculateDiskUsage_BuildCacheReclaimable(t *testing.T) {
	t.Parallel()

	usage := &client.DiskUsageResult{
		BuildCache: client.BuildCacheDiskUsage{Items: []build.CacheRecord{
			{ID: "unused", Size: 100},
			{ID: "in-use", Size: 20, InUse: true},
			{ID: "shared", Size: 7, Shared: true},
			{ID: "unused-2", Size: 1},
		}},
	}

	info := (&Collector{}).calculateDiskUsage(usage)
	if info.BuildCacheSize != 128 {
		t.Fatalf("build cache = %d, want 128", info.BuildCacheSize)
	}
	if info.BuildCacheReclaimable != 101 {
		t.Fatalf("build cache reclaimable = %d, want 101", info.BuildCacheReclaimable)
	}
	if info.ReclaimableSize != 101 {
		t.Fatalf("reclaimable = %d, want 101 (build cache only)", info.ReclaimableSize)
	}
}

func TestCalculateDiskUsage_BuildCacheAllInUse(t *testing.T) {
	t.Parallel()

	usage := &client.DiskUsageResult{
		BuildCache: client.BuildCacheDiskUsage{Items: []build.CacheRecord{
			{Size: 20, InUse: true},
			{Size: 7, Shared: true},
		}},
	}

	info := (&Collector{}).calculateDiskUsage(usage)
	if info.BuildCacheSize != 27 || info.BuildCacheReclaimable != 0 || info.ReclaimableSize != 0 {
		t.Fatalf("got size=%d reclaimable=%d total=%d, want 27/0/0",
			info.BuildCacheSize, info.BuildCacheReclaimable, info.ReclaimableSize)
	}
}

func TestMetricsFromSamples(t *testing.T) {
	t.Parallel()

	sample := func(total, system uint64) *container.StatsResponse {
		return &container.StatsResponse{
			CPUStats:    container.CPUStats{CPUUsage: container.CPUUsage{TotalUsage: total}, SystemUsage: system, OnlineCPUs: 2},
			MemoryStats: container.MemoryStats{Usage: 50, Limit: 200},
			Networks: map[string]container.NetworkStats{
				"eth0": {RxBytes: 100, TxBytes: 10},
				"eth1": {RxBytes: 5, TxBytes: 1},
			},
			BlkioStats: container.BlkioStats{IoServiceBytesRecursive: []container.BlkioStatEntry{
				{Op: "read", Value: 7}, {Op: "write", Value: 3}, {Op: "Read", Value: 1}, {Op: "Write", Value: 2},
			}},
		}
	}

	m := MetricsFromSamples("abc", sample(1000, 10000), sample(2000, 14000))
	// (1000 / 4000) * 2 CPUs * 100
	if m.CPUPercent != 50 {
		t.Fatalf("cpu = %v, want 50", m.CPUPercent)
	}
	if m.MemoryPercent != 25 {
		t.Fatalf("mem = %v, want 25", m.MemoryPercent)
	}
	if m.NetworkRx != 105 || m.NetworkTx != 11 {
		t.Fatalf("network rx/tx = %d/%d, want 105/11", m.NetworkRx, m.NetworkTx)
	}
	if m.BlockRead != 8 || m.BlockWrite != 5 {
		t.Fatalf("block read/write = %d/%d, want 8/5", m.BlockRead, m.BlockWrite)
	}

	if !SampleNotRunning(&container.StatsResponse{}) || SampleNotRunning(&container.StatsResponse{Read: time.Now()}) {
		t.Fatal("SampleNotRunning must key on the zero read time")
	}
}
