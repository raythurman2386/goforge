package deploy

import (
	"context"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// CleanupOldDeployments stops containers from previous deployments of the same environment.
func (s *Service) CleanupOldDeployments(ctx context.Context, environmentID, currentDeploymentID string) error {
	// Apply grace period if blue-green deployer is configured
	if s.blueGreenDeployer != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.blueGreenDeployer.config.GracePeriod):
			// Grace period elapsed, proceed with cleanup
		}
	}
	// 1. List recent deployments for the environment
	deployments, err := s.deploymentRepo.ListByEnvironmentID(ctx, environmentID, 10)
	if err != nil {
		s.logger.Error("failed to list deployments for cleanup", "environment_id", environmentID, "error", err)
		return err
	}

	for _, d := range deployments {
		// Skip the current deployment
		if d.ID == currentDeploymentID {
			continue
		}

		// Check if deployment was successful (Running)
		if d.Status == models.DeploymentStatusRunning {
			// Find containers associated with this deployment
			containers, err := s.containerRepo.ListByDeploymentID(ctx, d.ID)
			if err != nil {
				// Log error but continue
				s.logger.Warn("failed to list containers for deployment during cleanup", "deployment_id", d.ID, "error", err)
				continue
			}

			for _, c := range containers {
				// Only stop running containers
				if c.Status == models.ContainerStatusRunning {
					// Stop and remove container from Docker
					if err := s.dockerClient.StopContainer(ctx, c.DockerID); err != nil {
						s.logger.Warn("failed to stop container during cleanup", "container_id", c.DockerID, "error", err)
						// Continue anyway to update DB
					}

					// Update container status in DB
					if err := s.containerRepo.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); err != nil {
						s.logger.Warn("failed to update container status during cleanup", "container_id", c.ID, "error", err)
					}
				}
			}
		}
	}

	// Prune dangling images (optional, maybe run this async or periodically, but for now we do it here)
	// We don't error out if pruning fails
	if _, err := s.dockerClient.PruneImages(ctx); err != nil {
		s.logger.Warn("failed to prune images during cleanup", "error", err)
	}

	return nil
}
