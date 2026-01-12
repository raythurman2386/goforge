package deploy

import (
	"context"

	"github.com/user/gocool/internal/database/repositories"
	"github.com/user/gocool/internal/docker"
	"github.com/user/gocool/internal/git"
	"github.com/user/gocool/internal/models"
)

// Service handles deployment orchestration.
type Service struct {
	deploymentRepo *repositories.DeploymentRepository
	projectRepo    *repositories.ProjectRepository
	gitService     *git.Service
	dockerClient   *docker.Client
}

// NewService creates a new deployment Service.
func NewService(dr *repositories.DeploymentRepository, pr *repositories.ProjectRepository, gs *git.Service, dc *docker.Client) *Service {
	return &Service{
		deploymentRepo: dr,
		projectRepo:    pr,
		gitService:     gs,
		dockerClient:   dc,
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
