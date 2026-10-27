package deploy

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
)

// Interfaces below are the subset of repository and client methods the
// deployment pipeline uses. Concrete repositories and *docker.Client satisfy
// them; tests supply in-memory fakes.

type deploymentStore interface {
	Create(ctx context.Context, d *models.Deployment) error
	GetByID(ctx context.Context, id string) (*models.Deployment, error)
	UpdateStatus(ctx context.Context, id string, status models.DeploymentStatus, errorMsg *string) error
	ListByEnvironmentID(ctx context.Context, envID string, limit int) ([]*models.Deployment, error)
}

type deploymentLogStore interface {
	Create(ctx context.Context, log *models.DeploymentLog) error
	ListByDeploymentID(ctx context.Context, deploymentID string) ([]*models.DeploymentLog, error)
}

type projectStore interface {
	GetByID(ctx context.Context, id string) (*models.Project, error)
}

type environmentStore interface {
	GetByID(ctx context.Context, id string) (*models.Environment, error)
}

type containerStore interface {
	Create(ctx context.Context, c *models.Container) error
	ListByDeploymentID(ctx context.Context, deploymentID string) ([]*models.Container, error)
	UpdateStatus(ctx context.Context, id string, status models.ContainerStatus) error
}

type gitSourceStore interface {
	GetByID(ctx context.Context, id string) (*models.GitSource, error)
}

type envVarStore interface {
	ListByEnvironmentID(ctx context.Context, envID string) ([]*models.EnvVariable, error)
}

type linkedServiceStore interface {
	ListByProjectID(ctx context.Context, projectID string) ([]*models.ServiceInstance, error)
}

type gitCloner interface {
	Clone(ctx context.Context, opts git.CloneOptions) error
}

type containerRuntime interface {
	BuildImage(ctx context.Context, opts docker.BuildOptions, output io.Writer) error
	CreateContainer(ctx context.Context, opts docker.ContainerOptions) (string, error)
	InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error)
	StopContainer(ctx context.Context, id string) error
	PruneImages(ctx context.Context) (image.PruneReport, error)
}
