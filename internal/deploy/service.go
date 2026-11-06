// Package deploy handles deployment orchestration.
package deploy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
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
	transitionMu               sync.Mutex
	runsMu                     sync.Mutex
	runs                       map[string]context.CancelFunc
	retireRetry                time.Duration // 0: defaultRetireRetry
	lifecycle                  *docker.AppLifecycle
	containerCleanup           containerCleanupStore
}

// NewService creates a new deployment Service.
func NewService(
	dr deploymentStore,
	lr deploymentLogStore,
	pr projectStore,
	er environmentStore,
	cr containerStore,
	gsr gitSourceStore,
	evr envVarStore,
	psir linkedServiceStore,
	gs gitCloner,
	dc containerRuntime,
	sh *sse.Hub,
	traefikNetwork string,
	cloudflareProxyMode bool,
	buildDir string,
	logger *slog.Logger,
) *Service {
	var bgDeployer *BlueGreenDeployer
	if dockerClient, ok := dc.(*docker.Client); ok {
		bgConfig := DefaultBlueGreenConfig()
		bgDeployer = NewBlueGreenDeployer(dockerClient, &http.Client{Timeout: bgConfig.HealthCheckTimeout}, bgConfig)
	}

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

// Cancel marks a deployment canceled and stops its in-flight pipeline.
func (s *Service) Cancel(ctx context.Context, deploymentID, reason string) error {
	if err := s.Transition(ctx, deploymentID, models.DeploymentStatusCanceled, &reason); err != nil {
		return err
	}
	s.stopRun(deploymentID)
	return nil
}

func (s *Service) trackRun(deploymentID string, cancel context.CancelFunc) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	if s.runs == nil {
		s.runs = map[string]context.CancelFunc{}
	}
	s.runs[deploymentID] = cancel
}

func (s *Service) untrackRun(deploymentID string) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	delete(s.runs, deploymentID)
}

func (s *Service) stopRun(deploymentID string) {
	s.runsMu.Lock()
	cancel := s.runs[deploymentID]
	s.runsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

type containerCleanupStore interface {
	Retain(ctx context.Context, c *models.ContainerCleanup) error
}

// WithLifecycle shares exclusion between container creation/retirement and
// parent deletion. Callers hold the create lock through their DB recording.
func (s *Service) WithLifecycle(l *docker.AppLifecycle) *Service {
	s.lifecycle = l
	return s
}

// WithContainerCleanup retains removal work when create/rollback cleanup fails.
func (s *Service) WithContainerCleanup(store containerCleanupStore) *Service {
	s.containerCleanup = store
	return s
}

func (s *Service) createAppContainer(ctx context.Context, env *models.Environment, opts docker.ContainerOptions) (dockerID string, finish func(), err error) {
	unlock := func() {}
	if s.lifecycle != nil {
		release, lockErr := s.lifecycle.Create(env.ProjectID)
		if lockErr != nil {
			return "", nil, lockErr
		}
		unlock = release
	}
	// These fresh lookups close the delete-after-original-lookup window: the
	// parent must exist now, not only when the deployment started.
	currentEnv, err := s.environmentRepo.GetByID(ctx, env.ID)
	if err != nil {
		return "", unlock, fmt.Errorf("environment no longer available: %w", err)
	}
	if _, lookupErr := s.projectRepo.GetByID(ctx, currentEnv.ProjectID); lookupErr != nil {
		return "", unlock, fmt.Errorf("project no longer available: %w", lookupErr)
	}
	dockerID, err = s.dockerClient.CreateContainer(ctx, opts)
	if err != nil {
		if dockerID != "" {
			// CreateContainer returns an ID when start failed after creation.
			if removeErr := s.dockerClient.RemoveContainer(ctx, dockerID); removeErr != nil && !docker.IsNotFound(removeErr) {
				if retainErr := s.retainUnrecordedCleanup(ctx, dockerID, currentEnv); retainErr != nil {
					return "", nil, errors.Join(fmt.Errorf("create container: %w", err), fmt.Errorf("retain cleanup after failed removal: %w", retainErr))
				}
			}
		}
		return "", unlock, err
	}
	return dockerID, unlock, nil
}

func (s *Service) retainUnrecordedCleanup(ctx context.Context, dockerID string, env *models.Environment) error {
	if s.containerCleanup == nil {
		return errors.New("no durable container cleanup store configured")
	}
	if err := s.containerCleanup.Retain(context.WithoutCancel(ctx), &models.ContainerCleanup{
		DockerID: dockerID, ProjectID: sql.NullString{String: env.ProjectID, Valid: env.ProjectID != ""},
		EnvironmentID: sql.NullString{String: env.ID, Valid: env.ID != ""},
		Reason:        models.ContainerCleanupCreateFailure,
	}); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to retain unrecorded container cleanup", "error", err, "docker_id", dockerID)
		}
		return err
	}
	return nil
}
