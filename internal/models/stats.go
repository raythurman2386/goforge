package models

// SystemStats contains high-level dashboard statistics.
type SystemStats struct {
	TotalProjects int
	TotalServices int

	// Aggregated resource usage across all containers
	TotalCPUUsage      float64 // Total CPU usage percentage
	TotalMemoryUsage   int64   // Total memory usage in bytes
	TotalMemoryLimit   int64   // Total memory limit in bytes
	MemoryUsagePercent float64 // Memory usage percentage

	// Docker disk usage
	DiskUsage *DiskUsageInfo

	// Container counts
	RunningContainers int
	TotalContainers   int
}

// DiskUsageInfo contains Docker disk usage information.
type DiskUsageInfo struct {
	ImagesSize      int64 // Total size of images
	ContainersSize  int64 // Total size of containers
	VolumesSize     int64 // Total size of volumes
	BuildCacheSize  int64 // Total size of build cache
	ReclaimableSize int64 // Total reclaimable space
}
