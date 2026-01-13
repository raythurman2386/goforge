// Package monitor provides container resource monitoring and stats collection.
package monitor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/raythurman2386/gocool/internal/database/repositories"
	"github.com/raythurman2386/gocool/internal/docker"
	"github.com/raythurman2386/gocool/internal/models"
)

// Collector handles container metrics collection.
type Collector struct {
	dockerClient  *docker.Client
	containerRepo *repositories.ContainerRepository
}

// NewCollector creates a new metrics collector.
func NewCollector(dc *docker.Client, cr *repositories.ContainerRepository) *Collector {
	return &Collector{
		dockerClient:  dc,
		containerRepo: cr,
	}
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
	// Basic calculation logic
	cpuPercent := 0.0

	memUsage := int64(v.MemoryStats.Usage) //nolint:gosec // usage fits in int64
	memLimit := int64(v.MemoryStats.Limit) //nolint:gosec // limit fits in int64
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
