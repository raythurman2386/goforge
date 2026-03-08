// Package monitor provides container resource monitoring and stats collection.
package monitor

import (
	"context"
	"encoding/json"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"golang.org/x/sync/errgroup"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// hostMemoryBytes returns the total physical RAM of the host in bytes.
// Falls back to 0 if the syscall is unavailable (non-Linux builds).
func hostMemoryBytes() int64 {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return 0
	}
	return int64(info.Totalram) * int64(info.Unit)
}

// Collector handles container metrics collection.
type Collector struct {
	dockerClient        *docker.Client
	containerRepo       *repositories.ContainerRepository
	projectRepo         *repositories.ProjectRepository
	serviceInstanceRepo *repositories.ServiceInstanceRepository
}

// NewCollector creates a new metrics collector.
func NewCollector(
	dc *docker.Client,
	cr *repositories.ContainerRepository,
	pr *repositories.ProjectRepository,
	sr *repositories.ServiceInstanceRepository,
) *Collector {
	return &Collector{
		dockerClient:        dc,
		containerRepo:       cr,
		projectRepo:         pr,
		serviceInstanceRepo: sr,
	}
}

// CollectSystemStats collects high-level system stats for a user.
func (c *Collector) CollectSystemStats(ctx context.Context, userID string) (*models.SystemStats, error) {
	stats := &models.SystemStats{}
	g, gCtx := errgroup.WithContext(ctx)

	// 1. Projects count
	g.Go(func() error {
		projects, err := c.projectRepo.CountByUserID(gCtx, userID)
		if err == nil {
			stats.TotalProjects = projects
		}
		return nil // swallow DB error to allow partial dashboard rendering
	})

	// 2. Services count
	g.Go(func() error {
		services, err := c.serviceInstanceRepo.CountByUserID(gCtx, userID)
		if err == nil {
			stats.TotalServices = services
		}
		return nil // swallow DB error to allow partial dashboard rendering
	})

	// 3. Docker metrics (containers & stats)
	g.Go(func() error {
		allContainers, err := c.dockerClient.ListContainers(gCtx, true)
		if err != nil {
			// Ignore Docker list error to allow page load with empty stats
			return nil //nolint:nilerr // we intentionally swallow docker error to not break the dashboard
		}

		stats.TotalContainers = len(allContainers)

		var runningContainers []types.Container
		for i := range allContainers {
			if allContainers[i].State == "running" {
				runningContainers = append(runningContainers, allContainers[i])
			}
		}
		stats.RunningContainers = len(runningContainers)

		var mu sync.Mutex
		var metricsGroup errgroup.Group

		for i := range runningContainers {
			if runningContainers[i].ID == "" {
				continue
			}
			containerID := runningContainers[i].ID
			metricsGroup.Go(func() error {
				containerStats, statsErr := c.CollectStats(gCtx, containerID)
				if statsErr == nil {
					mu.Lock()
					stats.TotalCPUUsage += containerStats.CPUPercent
					stats.TotalMemoryUsage += containerStats.MemoryUsage
					mu.Unlock()
				}
				return nil
			})
		}

		if err := metricsGroup.Wait(); err != nil {
			return nil //nolint:nilerr // ignoring errcheck on wait
		}

		stats.TotalMemoryLimit = hostMemoryBytes()
		if stats.TotalMemoryLimit > 0 {
			stats.MemoryUsagePercent = float64(stats.TotalMemoryUsage) / float64(stats.TotalMemoryLimit) * 100
		}
		return nil
	})

	// 4. Docker disk usage
	g.Go(func() error {
		diskUsage, err := c.dockerClient.DiskUsage(gCtx)
		if err == nil && diskUsage != nil {
			usage := c.calculateDiskUsage(diskUsage)
			stats.DiskUsage = usage
		}
		return nil
	})

	// All goroutines swallow errors to allow partial stats, so Wait should
	// never return an error. We still check defensively.
	if err := g.Wait(); err != nil {
		return stats, nil //nolint:nilerr // partial stats are acceptable
	}

	return stats, nil
}

// calculateDiskUsage extracts disk usage information from Docker.
func (c *Collector) calculateDiskUsage(diskUsage *types.DiskUsage) *models.DiskUsageInfo {
	info := &models.DiskUsageInfo{}

	// Calculate total sizes
	for i := range diskUsage.Images {
		info.ImagesSize += diskUsage.Images[i].Size
	}

	for i := range diskUsage.Containers {
		if diskUsage.Containers[i].SizeRw > 0 {
			info.ContainersSize += diskUsage.Containers[i].SizeRw
		}
	}

	for i := range diskUsage.Volumes {
		if diskUsage.Volumes[i] != nil && diskUsage.Volumes[i].UsageData != nil {
			info.VolumesSize += diskUsage.Volumes[i].UsageData.Size
		}
	}

	for i := range diskUsage.BuildCache {
		if diskUsage.BuildCache[i] != nil {
			info.BuildCacheSize += diskUsage.BuildCache[i].Size
		}
	}

	// Calculate reclaimable space
	// 1. Unused images
	for i := range diskUsage.Images {
		if diskUsage.Images[i].Containers == 0 {
			info.ReclaimableSize += diskUsage.Images[i].Size
		}
	}

	// 2. Stopped containers
	for i := range diskUsage.Containers {
		if diskUsage.Containers[i].State != "running" && diskUsage.Containers[i].SizeRw > 0 {
			info.ReclaimableSize += diskUsage.Containers[i].SizeRw
		}
	}

	return info
}

// CollectStats collects stats for a container once.
func (c *Collector) CollectStats(ctx context.Context, dockerID string) (*models.ContainerMetrics, error) {
	stats, err := c.dockerClient.GetStats(ctx, dockerID)
	if err != nil {
		return nil, err
	}
	defer stats.Body.Close()

	var v container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&v); err != nil {
		return nil, err
	}

	return c.calculateMetrics(dockerID, &v), nil
}

func (c *Collector) calculateMetrics(dockerID string, v *container.StatsResponse) *models.ContainerMetrics {
	// Calculate CPU Percent
	// Formula: (cpuDelta / systemDelta) * numCores * 100.0
	cpuPercent := 0.0
	cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage) - float64(v.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(v.CPUStats.SystemUsage) - float64(v.PreCPUStats.SystemUsage)

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		// On cgroups v2, PercpuUsage is empty; fall back to OnlineCPUs.
		numCPUs := len(v.CPUStats.CPUUsage.PercpuUsage)
		if numCPUs == 0 {
			numCPUs = int(v.CPUStats.OnlineCPUs)
		}
		if numCPUs > 0 {
			cpuPercent = (cpuDelta / systemDelta) * float64(numCPUs) * 100.0
		}
	}

	memUsage := int64(v.MemoryStats.Usage)
	memLimit := int64(v.MemoryStats.Limit)
	memPercent := 0.0
	if memLimit > 0 {
		memPercent = float64(memUsage) / float64(memLimit) * 100.0
	}

	return &models.ContainerMetrics{
		ContainerID:   dockerID,
		Timestamp:     time.Now(),
		CPUPercent:    cpuPercent,
		MemoryUsage:   memUsage,
		MemoryLimit:   memLimit,
		MemoryPercent: memPercent,
	}
}
