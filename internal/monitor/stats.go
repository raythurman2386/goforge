// Package monitor provides container resource monitoring and stats collection.
package monitor

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
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

		var runningContainers []container.Summary
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

// calculateDiskUsage extracts disk usage for objects GoForge created.
// Images and containers must carry goforge.managed=true. Volumes are included
// when their name contains "goforge". Build cache cannot be filtered by label,
// so that total is the engine-wide cache size.
func (c *Collector) calculateDiskUsage(diskUsage *client.DiskUsageResult) *models.DiskUsageInfo {
	info := &models.DiskUsageInfo{}
	if diskUsage == nil {
		return info
	}

	for _, img := range diskUsage.Images.Items {
		if !managedDockerLabels(img.Labels) {
			continue
		}
		info.ImagesSize += img.Size
		if img.Containers == 0 {
			info.ReclaimableSize += img.Size
		}
	}

	for _, ctr := range diskUsage.Containers.Items {
		if !managedDockerLabels(ctr.Labels) || ctr.SizeRw <= 0 {
			continue
		}
		info.ContainersSize += ctr.SizeRw
		if ctr.State != "running" {
			info.ReclaimableSize += ctr.SizeRw
		}
	}

	for _, vol := range diskUsage.Volumes.Items {
		if vol.UsageData == nil || !strings.Contains(vol.Name, "goforge") {
			continue
		}
		info.VolumesSize += vol.UsageData.Size
	}

	for _, cache := range diskUsage.BuildCache.Items {
		info.BuildCacheSize += cache.Size
	}

	return info
}

func managedDockerLabels(labels map[string]string) bool {
	return labels[docker.ManagedLabelKey] == docker.ManagedLabelValue
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
