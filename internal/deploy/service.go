// Package deploy handles deployment orchestration.
package deploy

import (
	"context"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/sse"
)

// Service handles deployment orchestration.
type Service struct {
	deploymentRepo  *repositories.DeploymentRepository
	logRepo         *repositories.DeploymentLogRepository
	projectRepo     *repositories.ProjectRepository
	environmentRepo *repositories.EnvironmentRepository
	containerRepo   *repositories.ContainerRepository
	gitSourceRepo   *repositories.GitSourceRepository
	gitService      *git.Service
	dockerClient    *docker.Client
	sseHub          *sse.Hub
	traefikNetwork  string
}

// NewService creates a new deployment Service.
func NewService(
	dr *repositories.DeploymentRepository,
	lr *repositories.DeploymentLogRepository,
	pr *repositories.ProjectRepository,
	er *repositories.EnvironmentRepository,
	cr *repositories.ContainerRepository,
	gsr *repositories.GitSourceRepository,
	gs *git.Service,
	dc *docker.Client,
	sh *sse.Hub,
	traefikNetwork string,
) *Service {
	return &Service{
		deploymentRepo:  dr,
		logRepo:         lr,
		projectRepo:     pr,
		environmentRepo: er,
		containerRepo:   cr,
		gitSourceRepo:   gsr,
		gitService:      gs,
		dockerClient:    dc,
		sseHub:          sh,
		traefikNetwork:  traefikNetwork,
	}
}

// CreateDeployment creates a new deployment record.
func (s *Service) CreateDeployment(ctx context.Context, d *models.Deployment) error {
	return s.deploymentRepo.Create(ctx, d)
}

// GetDeployment retrieves a deployment by ID.
func (s *Service) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	return s.deploymentRepo.GetByID(ctx, id)
}

// UpdateStatus updates the status of a deployment.
func (s *Service) UpdateStatus(ctx context.Context, id string, status models.DeploymentStatus, errorMsg *string) error {
	return s.deploymentRepo.UpdateStatus(ctx, id, status, errorMsg)
}

// GetLogs retrieves logs for a deployment.
func (s *Service) GetLogs(ctx context.Context, deploymentID string) ([]*models.DeploymentLog, error) {
	return s.logRepo.ListByDeploymentID(ctx, deploymentID)
}

// Cancel cancels an in-progress deployment.
func (s *Service) Cancel(ctx context.Context, deploymentID, reason string) error {
	return s.Transition(ctx, deploymentID, models.DeploymentStatusCanceled, &reason)
}
