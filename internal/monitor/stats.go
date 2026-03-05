// Package monitor provides container resource monitoring and stats collection.
package monitor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

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
	projects, err := c.projectRepo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	services, err := c.serviceInstanceRepo.CountByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get all containers for resource aggregation
	containers, err := c.containerRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// Aggregate resource usage across all containers
	stats := &models.SystemStats{
		TotalProjects: projects,
		TotalServices: services,
	}

	// Collect container metrics and counts
	c.collectContainerMetrics(ctx, containers, stats)

	// Get Docker disk usage
	diskUsage, err := c.dockerClient.DiskUsage(ctx)
	if err == nil && diskUsage != nil {
		stats.DiskUsage = c.calculateDiskUsage(diskUsage)
	}

	return stats, nil
}

// collectContainerMetrics aggregates metrics from running containers.
func (c *Collector) collectContainerMetrics(ctx context.Context, containers []*models.Container, stats *models.SystemStats) {
	for _, cont := range containers {
		if cont.Status == models.ContainerStatusRunning {
			stats.RunningContainers++
		}
		stats.TotalContainers++

		// Try to get stats for running containers
		if cont.Status == models.ContainerStatusRunning && cont.DockerID != "" {
			containerStats, statsErr := c.CollectStats(ctx, cont.DockerID)
			if statsErr == nil {
				stats.TotalCPUUsage += containerStats.CPUPercent
				stats.TotalMemoryUsage += containerStats.MemoryUsage
				stats.TotalMemoryLimit += containerStats.MemoryLimit
			}
		}
	}

	// Calculate memory usage percentage
	if stats.TotalMemoryLimit > 0 {
		stats.MemoryUsagePercent = float64(stats.TotalMemoryUsage) / float64(stats.TotalMemoryLimit) * 100
	}
}

// calculateDiskUsage extracts disk usage information from Docker.
func (c *Collector) calculateDiskUsage(diskUsage *types.DiskUsage) *models.DiskUsageInfo {
	info := &models.DiskUsageInfo{}

	// Calculate total sizes
	for _, img := range diskUsage.Images {
		info.ImagesSize += img.Size
	}

	for _, cont := range diskUsage.Containers {
		info.ContainersSize += cont.SizeRw
	}

	for _, vol := range diskUsage.Volumes {
		if vol != nil && vol.UsageData != nil {
			info.VolumesSize += vol.UsageData.Size
		}
	}

	for _, cache := range diskUsage.BuildCache {
		if cache != nil {
			info.BuildCacheSize += cache.Size
		}
	}

	// Calculate reclaimable space
	for _, img := range diskUsage.Images {
		info.ReclaimableSize += img.SharedSize
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
		cpuPercent = (cpuDelta / systemDelta) * float64(len(v.CPUStats.CPUUsage.PercpuUsage)) * 100.0
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
