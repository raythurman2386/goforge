// Package deploy handles deployment orchestration.
package deploy

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/sse"
)

// Service handles deployment orchestration.
type Service struct {
	deploymentRepo             deploymentStore
	logRepo                    deploymentLogStore
	projectRepo                projectStore
	environmentRepo            environmentStore
	containerRepo              containerStore
	gitSourceRepo              gitSourceStore
	envVariableRepo            envVarStore
	projectServiceInstanceRepo linkedServiceStore
	gitService                 gitCloner
	dockerClient               containerRuntime
	sseHub                     *sse.Hub
	blueGreenDeployer          *BlueGreenDeployer
	traefikNetwork             string
	cloudflareProxyMode        bool
	buildDir                   string
	logger                     *slog.Logger
}

// NewService creates a new deployment Service.
func NewService(
	dr *repositories.DeploymentRepository,
	lr *repositories.DeploymentLogRepository,
	pr *repositories.ProjectRepository,
	er *repositories.EnvironmentRepository,
	cr *repositories.ContainerRepository,
	gsr *repositories.GitSourceRepository,
	evr *repositories.EnvVariableRepository,
	psir *repositories.ProjectServiceInstanceRepository,
	gs *git.Service,
	dc *docker.Client,
	sh *sse.Hub,
	traefikNetwork string,
	cloudflareProxyMode bool,
	buildDir string,
	logger *slog.Logger,
) *Service {
	// Initialize BlueGreenDeployer
	bgConfig := DefaultBlueGreenConfig()
	bgDeployer := NewBlueGreenDeployer(dc, &http.Client{Timeout: bgConfig.HealthCheckTimeout}, bgConfig)

	return &Service{
		deploymentRepo:             dr,
		logRepo:                    lr,
		projectRepo:                pr,
		environmentRepo:            er,
		containerRepo:              cr,
		gitSourceRepo:              gsr,
		envVariableRepo:            evr,
		projectServiceInstanceRepo: psir,
		gitService:                 gs,
		dockerClient:               dc,
		sseHub:                     sh,
		blueGreenDeployer:          bgDeployer,
		traefikNetwork:             traefikNetwork,
		cloudflareProxyMode:        cloudflareProxyMode,
		buildDir:                   buildDir,
		logger:                     logger,
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
	if s.logRepo == nil {
		return nil, nil
	}
	return s.logRepo.ListByDeploymentID(ctx, deploymentID)
}

// Cancel cancels an in-progress deployment.
func (s *Service) Cancel(ctx context.Context, deploymentID, reason string) error {
	return s.Transition(ctx, deploymentID, models.DeploymentStatusCanceled, &reason)
}
