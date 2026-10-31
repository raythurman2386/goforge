package deploy

import (
	"context"
	"errors"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/raythurman2386/goforge/internal/models"
)

const (
	// defaultRetireRetry is how long a cleanup waits before it tries again to
	// retire a container that Docker failed to remove.
	defaultRetireRetry = 30 * time.Second
	// retireAttemptTimeout bounds one pass of retireSuperseded, so a hung
	// Docker call ends that pass instead of the whole cleanup.
	retireAttemptTimeout = 2 * time.Minute
)

// errRetirePending means at least one superseded container could not be
// removed in this pass; its row is still running, so the next pass (or
// RecoverPendingRetires after a restart) tries it again.
var errRetirePending = errors.New("superseded container not retired yet")

// CleanupOldDeployments retires containers from previous deployments of the
// same environment. After the blue-green grace period it stops and then
// removes each running container of an earlier successful deployment, so a
// redeploy replaces the old container instead of leaving it on the host.
// The container row is kept (marked exited) because rollback rebuilds from it.
// If Docker fails to remove a container, its row stays running and the
// cleanup tries again every retireRetry until it is gone or ctx ends.
func (s *Service) CleanupOldDeployments(ctx context.Context, environmentID, currentDeploymentID string) error {
	if err := waitGrace(ctx, s.gracePeriod()); err != nil {
		return err
	}
	if _, err := s.retireUntilDone(ctx, environmentID, currentDeploymentID); err != nil {
		return err
	}
	s.pruneImages(ctx)
	return nil
}

// retireUntilDone runs retireSuperseded until a pass retires every superseded
// container, currentDeploymentID is no longer running (rolled back or itself
// retired by a newer cleanup), or ctx ends. It returns how many it retired.
func (s *Service) retireUntilDone(ctx context.Context, environmentID, currentDeploymentID string) (int, error) {
	total := 0
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, retireAttemptTimeout)
		n, err := s.retireSuperseded(attemptCtx, environmentID, currentDeploymentID)
		cancel()
		total += n
		if err == nil {
			return total, nil
		}
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		if s.logger != nil {
			s.logger.Warn("retire of previous deployment incomplete, will retry",
				"environment_id", environmentID, "current_deployment_id", currentDeploymentID,
				"retry_in", s.retireRetryInterval(), "error", err)
		}
		if err := waitGrace(ctx, s.retireRetryInterval()); err != nil {
			return total, err
		}
		if cur, gErr := s.deploymentRepo.GetByID(ctx, currentDeploymentID); gErr == nil && cur.Status != models.DeploymentStatusRunning {
			return total, nil
		}
	}
}

func (s *Service) retireRetryInterval() time.Duration {
	if s.retireRetry > 0 {
		return s.retireRetry
	}
	return defaultRetireRetry
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
		if cur, ok := current[d.EnvironmentID]; !ok || newerDeployment(d, cur) {
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
			n, err := s.retireUntilDone(ctx, envID, currentID)
			if err != nil && ctx.Err() == nil && s.logger != nil {
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

// newerDeployment reports whether a was created after b. Equal created_at
// (not seen in practice) is broken by ID so every caller agrees on one order.
func newerDeployment(a, b *models.Deployment) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// retireSuperseded retires the running containers of every running deployment
// of the environment that is older than the newest deployment whose grace is
// over: currentDeploymentID (whose grace the caller has just waited out), or a
// newer running deployment whose grace has also passed. It never touches a
// deployment newer than that one, so the cleanup of an older redeploy cannot
// retire the container of a newer one that is still inside its own grace or
// is the one serving. It returns how many containers it retired, and
// errRetirePending if Docker failed to remove one (its row stays running).
func (s *Service) retireSuperseded(ctx context.Context, environmentID, currentDeploymentID string) (int, error) {
	// Every running deployment of the environment, not the newest N rows: a
	// run of failed or canceled deploys must not push an older running one
	// out of the list and leave its container running for good.
	running, err := s.deploymentRepo.ListByStatus(ctx, models.DeploymentStatusRunning)
	if err != nil {
		s.logger.Error("failed to list deployments for cleanup", "environment_id", environmentID, "error", err)
		return 0, err
	}
	deployments := make([]*models.Deployment, 0, len(running))
	for _, d := range running {
		if d.EnvironmentID == environmentID {
			deployments = append(deployments, d)
		}
	}

	var cur *models.Deployment
	for _, d := range deployments {
		if d.ID == currentDeploymentID {
			cur = d
			break
		}
	}
	if cur == nil {
		if cur, err = s.deploymentRepo.GetByID(ctx, currentDeploymentID); err != nil {
			s.logger.Error("failed to load current deployment for cleanup", "deployment_id", currentDeploymentID, "error", err)
			return 0, err
		}
	}

	// keep is the newest running deployment whose grace is over; everything
	// older than it is superseded.
	keep, grace, now := cur, s.gracePeriod(), time.Now()
	for _, d := range deployments {
		if d.Status == models.DeploymentStatusRunning && newerDeployment(d, keep) &&
			now.Sub(runningSince(d)) >= grace {
			keep = d
		}
	}

	retired, pending := 0, false
	for _, d := range deployments {
		if d.ID == keep.ID || d.Status != models.DeploymentStatusRunning || !newerDeployment(keep, d) {
			continue
		}
		containers, err := s.containerRepo.ListByDeploymentID(ctx, d.ID)
		if err != nil {
			s.logger.Warn("failed to list containers for deployment during cleanup", "deployment_id", d.ID, "error", err)
			pending = true
			continue
		}
		for _, c := range containers {
			if c.Status != models.ContainerStatusRunning {
				continue
			}
			if s.retireContainer(ctx, d.ID, currentDeploymentID, c) {
				retired++
			} else {
				pending = true
			}
		}
	}
	if pending {
		return retired, errRetirePending
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
// Docker and, once it is gone from the host, marks its row exited. Stopping
// alone left the old container on the host after every redeploy, so remove
// always runs, even if stop fails. If remove fails (other than "no such
// container") the row stays running and it returns false, so the cleanup
// retries and hasSuperseded still finds it after a restart; marking it exited
// anyway left the old container running forever.
func (s *Service) retireContainer(ctx context.Context, deploymentID, currentDeploymentID string, c *models.Container) bool {
	if err := s.dockerClient.StopContainer(ctx, c.DockerID); err != nil {
		s.logger.Warn("failed to stop container during cleanup", "container_id", c.DockerID, "error", err)
	}
	if err := s.dockerClient.RemoveContainer(ctx, c.DockerID); err != nil && !cerrdefs.IsNotFound(err) {
		s.logger.Warn("failed to remove container during cleanup, row left running for retry",
			"container_id", c.DockerID, "error", err)
		return false
	}
	if s.logger != nil {
		s.logger.Info("retired previous deployment container",
			"deployment_id", deploymentID, "container_id", c.DockerID, "current_deployment_id", currentDeploymentID)
	}

	if err := s.containerRepo.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); err != nil {
		s.logger.Warn("failed to update container status during cleanup", "container_id", c.ID, "error", err)
	}
	return true
}
