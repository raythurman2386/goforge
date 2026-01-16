package deploy

import (
	"context"
	"fmt"

	"github.com/raythurman2386/goforge/internal/models"
)

// CleanupOldDeployments stops containers from previous deployments of the same environment.
func (s *Service) CleanupOldDeployments(ctx context.Context, environmentID string, currentDeploymentID string) error {
	// 1. List recent deployments for the environment
	deployments, err := s.deploymentRepo.ListByEnvironmentID(ctx, environmentID, 10)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
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
				fmt.Printf("failed to list containers for deployment %s: %v\n", d.ID, err)
				continue
			}

			for _, c := range containers {
				// Only stop running containers
				if c.Status == models.ContainerStatusRunning {
					// Stop and remove container from Docker
					if err := s.dockerClient.StopContainer(ctx, c.DockerID); err != nil {
						fmt.Printf("failed to stop container %s: %v\n", c.DockerID, err)
						// Continue anyway to update DB
					}

					// Update container status in DB
					if err := s.containerRepo.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); err != nil {
						fmt.Printf("failed to update container status %s: %v\n", c.ID, err)
					}
				}
			}
		}
	}
	
	// Prune dangling images (optional, maybe run this async or periodically, but for now we do it here)
	// We don't error out if pruning fails
	if _, err := s.dockerClient.PruneImages(ctx); err != nil {
		fmt.Printf("failed to prune images: %v\n", err)
	}

	return nil
}
