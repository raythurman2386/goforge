package deploy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/raythurman2386/gocool/internal/docker"
	"github.com/raythurman2386/gocool/internal/models"
)

// Rollback related errors.
var (
	ErrNoRollbackTarget   = errors.New("no rollback target found")
	ErrRollbackInProgress = errors.New("rollback already in progress")
	ErrCannotRollback     = errors.New("cannot rollback deployment")
)

// RollbackInfo contains information about a rollback operation.
type RollbackInfo struct {
	FromDeploymentID string
	ToDeploymentID   string
	Reason           string
	InitiatedBy      string
	InitiatedAt      time.Time
}

// String returns a string representation of the rollback info.
func (r *RollbackInfo) String() string {
	return fmt.Sprintf("rollback from %s to %s (reason: %s)", r.FromDeploymentID, r.ToDeploymentID, r.Reason)
}

// RollbackManager handles rollback operations for deployments.
type RollbackManager struct {
	service *Service
}

// NewRollbackManager creates a new RollbackManager.
func NewRollbackManager(service *Service) *RollbackManager {
	return &RollbackManager{
		service: service,
	}
}

// CanRollback determines if a deployment can be rolled back.
// Only terminal deployments (running, failed, canceled) can be rolled back.
// Deployments that are in progress or already rolled back cannot be rolled back.
func (rm *RollbackManager) CanRollback(deployment *models.Deployment) bool {
	if deployment == nil {
		return false
	}

	switch deployment.Status {
	case models.DeploymentStatusRunning,
		models.DeploymentStatusFailed,
		models.DeploymentStatusCanceled:
		return true
	case models.DeploymentStatusPending,
		models.DeploymentStatusCloning,
		models.DeploymentStatusBuilding,
		models.DeploymentStatusDeploying,
		models.DeploymentStatusRolledBack:
		return false
	default:
		return false
	}
}

// FindRollbackTarget finds the most recent successful deployment to rollback to.
// It skips the current deployment and any failed/rolled-back deployments.
func (rm *RollbackManager) FindRollbackTarget(deployments []*models.Deployment, currentID string) (*models.Deployment, error) {
	foundCurrent := false

	for _, d := range deployments {
		// Skip until we find the current deployment
		if d.ID == currentID {
			foundCurrent = true
			continue
		}

		// Only consider deployments after the current one
		if !foundCurrent {
			continue
		}

		// Only consider successful (running) deployments as rollback targets
		if d.Status == models.DeploymentStatusRunning {
			return d, nil
		}
	}

	return nil, ErrNoRollbackTarget
}

// ValidateRollback validates that a rollback operation is valid.
func (rm *RollbackManager) ValidateRollback(current, target *models.Deployment) error {
	if current == nil {
		return fmt.Errorf("%w: current deployment is nil", ErrCannotRollback)
	}

	if target == nil {
		return fmt.Errorf("%w: target deployment is nil", ErrCannotRollback)
	}

	if current.ID == target.ID {
		return fmt.Errorf("%w: cannot rollback to the same deployment", ErrCannotRollback)
	}

	if current.EnvironmentID != target.EnvironmentID {
		return fmt.Errorf("%w: deployments belong to different environments", ErrCannotRollback)
	}

	if target.Status != models.DeploymentStatusRunning {
		return fmt.Errorf("%w: target deployment was not successful", ErrCannotRollback)
	}

	return nil
}

// CreateRollbackDeployment creates a new deployment for the rollback operation.
// It copies relevant fields from the target deployment.
func (rm *RollbackManager) CreateRollbackDeployment(_ context.Context, target *models.Deployment, reason string) *models.Deployment {
	return &models.Deployment{
		EnvironmentID: target.EnvironmentID,
		Status:        models.DeploymentStatusPending,
		TriggerType:   models.TriggerTypeRollback,
		CommitSHA:     target.CommitSHA,
		CommitMessage: sql.NullString{String: fmt.Sprintf("Rollback: %s", reason), Valid: true},
		CommitAuthor:  target.CommitAuthor,
		Branch:        target.Branch,
		ImageTag:      target.ImageTag,
	}
}

// Rollback performs a rollback operation.
// It stops the current deployment's containers and starts the target deployment's containers.
func (rm *RollbackManager) Rollback(ctx context.Context, environmentID, reason string) (*RollbackInfo, error) {
	if rm.service == nil {
		return nil, fmt.Errorf("%w: service not configured", ErrCannotRollback)
	}

	// Get all deployments for this environment
	deployments, err := rm.service.deploymentRepo.ListByEnvironmentID(ctx, environmentID, 50)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	if len(deployments) == 0 {
		return nil, ErrNoRollbackTarget
	}

	// Get the current (latest) deployment
	current := deployments[0]

	// Check if we can rollback
	if !rm.CanRollback(current) {
		return nil, fmt.Errorf("%w: deployment status is %s", ErrCannotRollback, current.Status)
	}

	// Find the target deployment
	target, err := rm.FindRollbackTarget(deployments, current.ID)
	if err != nil {
		return nil, err
	}

	// Validate the rollback
	if err := rm.ValidateRollback(current, target); err != nil {
		return nil, err
	}

	// Create rollback info
	info := &RollbackInfo{
		FromDeploymentID: current.ID,
		ToDeploymentID:   target.ID,
		Reason:           reason,
		InitiatedAt:      time.Now(),
	}

	// Stop current containers
	if err := rm.stopDeploymentContainers(ctx, current.ID); err != nil {
		return info, fmt.Errorf("failed to stop current containers: %w", err)
	}

	// Mark current deployment as rolled back
	if err := rm.service.Transition(ctx, current.ID, models.DeploymentStatusRolledBack, nil); err != nil {
		return info, fmt.Errorf("failed to update current deployment status: %w", err)
	}

	// Start the target deployment's container using its existing image
	if err := rm.restoreDeployment(ctx, target); err != nil {
		return info, fmt.Errorf("failed to restore target deployment: %w", err)
	}

	return info, nil
}

// RollbackTo performs a rollback to a specific deployment.
func (rm *RollbackManager) RollbackTo(ctx context.Context, currentID, targetID, reason string) (*RollbackInfo, error) {
	if rm.service == nil {
		return nil, fmt.Errorf("%w: service not configured", ErrCannotRollback)
	}

	// Get the current deployment
	current, err := rm.service.deploymentRepo.GetByID(ctx, currentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current deployment: %w", err)
	}

	// Get the target deployment
	target, err := rm.service.deploymentRepo.GetByID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target deployment: %w", err)
	}

	// Check if we can rollback
	if !rm.CanRollback(current) {
		return nil, fmt.Errorf("%w: deployment status is %s", ErrCannotRollback, current.Status)
	}

	// Validate the rollback
	if err := rm.ValidateRollback(current, target); err != nil {
		return nil, err
	}

	// Create rollback info
	info := &RollbackInfo{
		FromDeploymentID: current.ID,
		ToDeploymentID:   target.ID,
		Reason:           reason,
		InitiatedAt:      time.Now(),
	}

	// Stop current containers
	if err := rm.stopDeploymentContainers(ctx, current.ID); err != nil {
		return info, fmt.Errorf("failed to stop current containers: %w", err)
	}

	// Mark current deployment as rolled back
	if err := rm.service.Transition(ctx, current.ID, models.DeploymentStatusRolledBack, nil); err != nil {
		return info, fmt.Errorf("failed to update current deployment status: %w", err)
	}

	// Start the target deployment's container using its existing image
	if err := rm.restoreDeployment(ctx, target); err != nil {
		return info, fmt.Errorf("failed to restore target deployment: %w", err)
	}

	return info, nil
}

// stopDeploymentContainers stops all containers associated with a deployment.
func (rm *RollbackManager) stopDeploymentContainers(ctx context.Context, deploymentID string) error {
	containers, err := rm.service.containerRepo.ListByDeploymentID(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for _, container := range containers {
		if container.Status != models.ContainerStatusRunning {
			continue
		}

		if err := rm.service.dockerClient.StopContainer(ctx, container.DockerID); err != nil {
			// Log but continue - container might already be stopped
			fmt.Printf("warning: failed to stop container %s: %v\n", container.DockerID, err)
		}

		if err := rm.service.containerRepo.UpdateStatus(ctx, container.ID, models.ContainerStatusExited); err != nil {
			fmt.Printf("warning: failed to update container status: %v\n", err)
		}
	}

	return nil
}

// restoreDeployment restores a previous deployment by starting its container.
func (rm *RollbackManager) restoreDeployment(ctx context.Context, target *models.Deployment) error {
	// Get the containers from the target deployment
	containers, err := rm.service.containerRepo.ListByDeploymentID(ctx, target.ID)
	if err != nil {
		return fmt.Errorf("failed to list target containers: %w", err)
	}

	if len(containers) == 0 {
		return fmt.Errorf("no containers found for target deployment")
	}

	// Get the environment and project for Traefik labels
	env, err := rm.service.environmentRepo.GetByID(ctx, target.EnvironmentID)
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	project, err := rm.service.projectRepo.GetByID(ctx, env.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to get project: %w", err)
	}

	// Restore the first container (main application container)
	targetContainer := containers[0]

	// Create a new container with the same image
	_, err = rm.service.dockerClient.CreateContainer(ctx, containerOptionsFromContainer(targetContainer, project, env))
	if err != nil {
		return fmt.Errorf("failed to create restored container: %w", err)
	}

	return nil
}

// containerOptionsFromContainer creates container options from an existing container record.
func containerOptionsFromContainer(c *models.Container, _ *models.Project, _ *models.Environment) docker.ContainerOptions {
	labels, err := c.GetLabels()
	if err != nil {
		labels = make(map[string]string)
	}
	return docker.ContainerOptions{
		Name:   c.Name + "-restored",
		Image:  c.Image,
		Labels: labels,
	}
}
