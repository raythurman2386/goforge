package deploy

import (
	"context"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// CleanupOldDeployments retires containers from previous deployments of the
// same environment. After the blue-green grace period it stops and then
// removes each running container of an earlier successful deployment, so a
// redeploy replaces the old container instead of leaving it on the host.
// The container row is kept (marked exited) because rollback rebuilds from it.
func (s *Service) CleanupOldDeployments(ctx context.Context, environmentID, currentDeploymentID string) error {
	if err := waitGrace(ctx, s.gracePeriod()); err != nil {
		return err
	}
	if _, err := s.retireSuperseded(ctx, environmentID, currentDeploymentID); err != nil {
		return err
	}
	s.pruneImages(ctx)
	return nil
}

// RecoverPendingRetires finishes cleanups that a process restart dropped.
// The grace timer in CleanupOldDeployments lives only in memory, so a restart
// within the grace period left the previous container running beside the new
// one. On boot, for each environment whose newest running deployment still
// has an older running deployment with a running container, this waits out
// what is left of the grace period (measured from when the newest deployment
// became running) and then retires the older containers. It returns once
// every recovered environment is done or ctx is canceled.
func (s *Service) RecoverPendingRetires(ctx context.Context) error {
	running, err := s.deploymentRepo.ListByStatus(ctx, models.DeploymentStatusRunning)
	if err != nil {
		return err
	}

	// The newest running deployment of each environment is the current one.
	current := map[string]*models.Deployment{}
	for _, d := range running {
		if cur, ok := current[d.EnvironmentID]; !ok || d.CreatedAt.After(cur.CreatedAt) {
			current[d.EnvironmentID] = d
		}
	}

	var wg sync.WaitGroup
	for envID, cur := range current {
		if !s.hasSuperseded(ctx, running, envID, cur.ID) {
			continue
		}
		wait := s.gracePeriod() - time.Since(runningSince(cur))
		if s.logger != nil {
			s.logger.Info("recovering pending retire of previous deployment",
				"environment_id", envID, "current_deployment_id", cur.ID, "wait", wait.Round(time.Second))
		}
		wg.Add(1)
		go func(envID, currentID string, wait time.Duration) {
			defer wg.Done()
			if err := waitGrace(ctx, wait); err != nil {
				return
			}
			n, err := s.retireSuperseded(ctx, envID, currentID)
			if err != nil && s.logger != nil {
				s.logger.Error("failed to recover pending retire", "environment_id", envID, "error", err)
			}
			if n > 0 {
				s.pruneImages(ctx)
			}
		}(envID, cur.ID, wait)
	}
	wg.Wait()
	return ctx.Err()
}

// hasSuperseded reports whether an older running deployment of envID still
// has a running container row.
func (s *Service) hasSuperseded(ctx context.Context, running []*models.Deployment, envID, currentID string) bool {
	for _, d := range running {
		if d.EnvironmentID != envID || d.ID == currentID {
			continue
		}
		containers, err := s.containerRepo.ListByDeploymentID(ctx, d.ID)
		if err != nil {
			continue
		}
		for _, c := range containers {
			if c.Status == models.ContainerStatusRunning {
				return true
			}
		}
	}
	return false
}

// runningSince is when d reached running: finished_at is set on that
// transition. created_at is the fallback for rows without it.
func runningSince(d *models.Deployment) time.Time {
	if d.FinishedAt.Valid {
		return d.FinishedAt.Time
	}
	return d.CreatedAt
}

func (s *Service) gracePeriod() time.Duration {
	if s.blueGreenDeployer == nil {
		return 0
	}
	return s.blueGreenDeployer.config.GracePeriod
}

func waitGrace(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retireSuperseded retires every running container of an earlier running
// deployment in the environment's recent history and returns how many it
// retired.
func (s *Service) retireSuperseded(ctx context.Context, environmentID, currentDeploymentID string) (int, error) {
	deployments, err := s.deploymentRepo.ListByEnvironmentID(ctx, environmentID, 10)
	if err != nil {
		s.logger.Error("failed to list deployments for cleanup", "environment_id", environmentID, "error", err)
		return 0, err
	}

	retired := 0
	for _, d := range deployments {
		if d.ID == currentDeploymentID || d.Status != models.DeploymentStatusRunning {
			continue
		}
		containers, err := s.containerRepo.ListByDeploymentID(ctx, d.ID)
		if err != nil {
			s.logger.Warn("failed to list containers for deployment during cleanup", "deployment_id", d.ID, "error", err)
			continue
		}
		for _, c := range containers {
			if c.Status == models.ContainerStatusRunning {
				s.retireContainer(ctx, d.ID, currentDeploymentID, c)
				retired++
			}
		}
	}
	return retired, nil
}

// pruneImages removes dangling images; a failure is only logged.
func (s *Service) pruneImages(ctx context.Context) {
	if _, err := s.dockerClient.PruneImages(ctx); err != nil {
		s.logger.Warn("failed to prune images during cleanup", "error", err)
	}
}

// retireContainer stops and removes one previous deployment container in
// Docker and marks its row exited. Stopping alone left the old container on
// the host after every redeploy, so remove always runs, even if stop fails.
func (s *Service) retireContainer(ctx context.Context, deploymentID, currentDeploymentID string, c *models.Container) {
	if err := s.dockerClient.StopContainer(ctx, c.DockerID); err != nil {
		s.logger.Warn("failed to stop container during cleanup", "container_id", c.DockerID, "error", err)
	}
	if err := s.dockerClient.RemoveContainer(ctx, c.DockerID); err != nil {
		s.logger.Warn("failed to remove container during cleanup", "container_id", c.DockerID, "error", err)
	} else if s.logger != nil {
		s.logger.Info("retired previous deployment container",
			"deployment_id", deploymentID, "container_id", c.DockerID, "current_deployment_id", currentDeploymentID)
	}

	// Update container status in DB
	if err := s.containerRepo.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); err != nil {
		s.logger.Warn("failed to update container status during cleanup", "container_id", c.ID, "error", err)
	}
}
