package handlers

import (
	"context"
	"log/slog"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// containerInspector reads Docker inspect for one container; *docker.Client
// satisfies it.
type containerInspector interface {
	InspectContainer(ctx context.Context, id string) (container.InspectResponse, error)
}

// inspectSummary returns the shared secret-free inspect summary for c, or
// nil when there is no container, no inspector, or inspect fails.
func inspectSummary(ctx context.Context, insp containerInspector, c *models.Container, logger *slog.Logger) *docker.InspectSummary {
	if insp == nil || c == nil || c.DockerID == "" {
		return nil
	}
	info, err := insp.InspectContainer(ctx, c.DockerID)
	if err != nil {
		if logger != nil {
			logger.Debug("failed to inspect container", "docker_id", c.DockerID, "error", err)
		}
		return nil
	}
	return docker.SummarizeInspect(&info)
}

// containerLister lists the containers of one deployment.
type containerLister interface {
	ListByDeploymentID(ctx context.Context, deploymentID string) ([]*models.Container, error)
}

// deploymentLister lists an environment's deployments, newest first.
type deploymentLister interface {
	ListByEnvironmentID(ctx context.Context, envID string, limit int) ([]*models.Deployment, error)
}

// activeContainerLookback bounds how many recent deployments the project
// overview searches for a running container.
const activeContainerLookback = 20

// activeContainer picks the container the project overview streams. It is
// always a running container or nil, never a stopped or removed one:
//  1. a running container of the latest deployment;
//  2. else a running container of the newest successful (status running)
//     earlier deployment, which keeps the previous container's stats on
//     screen while a new deployment is in flight;
//  3. else nil, and the page shows an empty state.
func activeContainer(ctx context.Context, containers containerLister, deployments deploymentLister, envID string, latest *models.Deployment, logger *slog.Logger) *models.Container {
	if containers == nil || latest == nil {
		return nil
	}
	if c := runningContainerOf(ctx, containers, latest.ID, logger); c != nil {
		return c
	}
	if deployments == nil {
		return nil
	}
	recent, err := deployments.ListByEnvironmentID(ctx, envID, activeContainerLookback)
	if err != nil {
		logger.Warn("failed to list deployments for active container", "environment_id", envID, "error", err)
		return nil
	}
	for _, d := range recent {
		if d.ID == latest.ID || d.Status != models.DeploymentStatusRunning {
			continue
		}
		if c := runningContainerOf(ctx, containers, d.ID, logger); c != nil {
			return c
		}
	}
	return nil
}

func runningContainerOf(ctx context.Context, containers containerLister, deploymentID string, logger *slog.Logger) *models.Container {
	list, err := containers.ListByDeploymentID(ctx, deploymentID)
	if err != nil {
		logger.Warn("failed to list containers for deployment", "deployment_id", deploymentID, "error", err)
		return nil
	}
	for _, c := range list {
		if c.Status == models.ContainerStatusRunning {
			return c
		}
	}
	return nil
}
