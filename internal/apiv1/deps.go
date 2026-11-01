// Package apiv1 serves the bearer-token JSON API under /api/v1.
//
// The API is the backend for the GoForge MCP tools (/mcp via internal/mcphttp,
// and the cmd/goforge-mcp stdio fallback). It is
// bearer-only: session cookies are ignored, so it sits outside the CSRF group.
// Every route needs a token scope (read, write, or protected), and every
// mutation is gated: dry_run defaults to true, and a real run needs a
// non-empty approval_ref that is written to audit_logs before anything runs.
package apiv1

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

// Projects reads projects and environments and edits environment variables.
type Projects interface {
	GetProject(ctx context.Context, id string) (*models.Project, error)
	ListProjects(ctx context.Context, userID string) ([]*models.Project, error)
	ListEnvironments(ctx context.Context, projectID string) ([]*models.Environment, error)
	GetEnvironment(ctx context.Context, id string) (*models.Environment, error)
	ListEnvVariables(ctx context.Context, envID string) ([]*models.EnvVariable, error)
	AddEnvVariable(ctx context.Context, v *models.EnvVariable) error
	UpdateEnvVariable(ctx context.Context, v *models.EnvVariable) error
	DeleteEnvVariableScoped(ctx context.Context, id, environmentID string) error
	SeedEnvVarsFromRepo(ctx context.Context, envID, projectID string) (int, error)
}

// Deployments creates, reads, and cancels deployments.
type Deployments interface {
	CreateDeployment(ctx context.Context, d *models.Deployment) error
	GetDeployment(ctx context.Context, id string) (*models.Deployment, error)
	GetLogs(ctx context.Context, deploymentID string) ([]*models.DeploymentLog, error)
	Cancel(ctx context.Context, deploymentID, reason string) error
}

// DeploymentLister lists an environment's deployments, newest first.
type DeploymentLister interface {
	ListByEnvironmentID(ctx context.Context, envID string, limit int) ([]*models.Deployment, error)
}

// DeployQueue is the deployment worker queue.
type DeployQueue interface {
	Enqueue(ctx context.Context, deploymentID string) error
	DropPending(ctx context.Context, deploymentID string) error
}

// Rollbacker rolls an environment back to its previous good deployment.
type Rollbacker interface {
	CanRollback(d *models.Deployment) bool
	Rollback(ctx context.Context, envID, reason string) (*deploy.RollbackInfo, error)
}

// Instances manages one-click service instances.
type Instances interface {
	ListInstances(ctx context.Context, userID string) ([]*models.ServiceInstance, error)
	GetInstance(ctx context.Context, id string) (*models.ServiceInstance, error)
	GetContainer(ctx context.Context, instanceID string) (*models.Container, error)
	Template(name string) *services.ServiceTemplate
	DeployInstance(ctx context.Context, userID, tmplName string, config map[string]interface{}) (*models.ServiceInstance, error)
	UpdateInstanceConfig(ctx context.Context, id string, updates map[string]interface{}) (*models.ServiceInstance, error)
	UpdateInstanceImage(ctx context.Context, id string) (*models.ServiceInstance, error)
	RestartInstance(ctx context.Context, id string) error
}

// Catalog lists one-click service templates.
type Catalog interface {
	ListTemplates() []*services.ServiceTemplate
	GetTemplate(name string) (*services.ServiceTemplate, error)
	ReloadIfChanged() (bool, error)
}

// ServiceLinks links service instances to projects.
type ServiceLinks interface {
	Link(ctx context.Context, projectID, serviceInstanceID string) error
	Unlink(ctx context.Context, projectID, serviceInstanceID string) error
	ListByProjectID(ctx context.Context, projectID string) ([]*models.ServiceInstance, error)
}

// Containers reads tracked containers and checks their ownership.
type Containers interface {
	ListByUserID(ctx context.Context, userID string) ([]*models.Container, error)
	GetByDockerID(ctx context.Context, dockerID string) (*models.Container, error)
	OwnedByUser(ctx context.Context, dockerID, userID string) (bool, error)
	UpdateStatus(ctx context.Context, id string, status models.ContainerStatus) error
}

// Docker is the subset of the Docker client the API calls.
type Docker interface {
	ListContainers(ctx context.Context, all bool) ([]container.Summary, error)
	InspectContainer(ctx context.Context, id string) (container.InspectResponse, error)
	StopContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
	ContainerLogs(ctx context.Context, id string, opts docker.LogsOptions) (io.ReadCloser, error)
	Ping(ctx context.Context) error
}

// Stats collects dashboard and container metrics.
type Stats interface {
	CollectSystemStats(ctx context.Context, userID string) (*models.SystemStats, error)
	CollectStats(ctx context.Context, dockerID string) (*models.ContainerMetrics, error)
}

// AuditStore writes and lists audit_logs rows. Gated writes call Create
// synchronously so a failed audit insert stops the mutation.
type AuditStore interface {
	Create(ctx context.Context, l *models.AuditLog) error
	ListFiltered(ctx context.Context, f repositories.AuditFilter) ([]*models.AuditLog, error)
	// FindApproval returns the first approved-change row of userID with
	// this action, approval_ref, and idempotency_key, or nil when none.
	FindApproval(ctx context.Context, userID, action, approvalRef, idempotencyKey string) (*models.AuditLog, error)
	// FindOutcome returns the <action>.completed or <action>.failed row
	// written for auditID, or nil while the change is still running.
	FindOutcome(ctx context.Context, userID, action, auditID string) (*models.AuditLog, error)
}

// GitSources reads the user's git sources. Credentials are never returned.
type GitSources interface {
	ListByUserID(ctx context.Context, userID string) ([]*models.GitSource, error)
	GetByID(ctx context.Context, id string) (*models.GitSource, error)
}

// GitBrowser lists repositories and branches through a git source.
type GitBrowser interface {
	ListRepositories(ctx context.Context, source *models.GitSource) ([]*git.Repository, error)
	ListBranches(ctx context.Context, source *models.GitSource, fullName string) ([]*git.Branch, error)
}

// Backups reads the backup status. It never returns credentials.
type Backups interface {
	Status(ctx context.Context) (*backup.Status, error)
}

// DBHealth checks the database connection.
type DBHealth interface {
	HealthCheck(ctx context.Context) error
}

// Deps holds everything the API needs. Optional dependencies may be nil; the
// routes that need them answer 503.
type Deps struct {
	Tokens auth.TokenLookup
	Users  auth.UserLookup

	Projects    Projects
	Deployments Deployments
	DeployList  DeploymentLister
	Queue       DeployQueue
	Rollback    Rollbacker
	Instances   Instances
	Catalog     Catalog
	Links       ServiceLinks
	Containers  Containers
	Docker      Docker
	Stats       Stats
	Audit       AuditStore
	GitSources  GitSources
	GitBrowser  GitBrowser
	DB          DBHealth
	Backups     Backups
	Logger      *slog.Logger
	Version     string
	// ClientIP returns the caller address recorded in audit_logs. Nil uses
	// the TCP peer address.
	ClientIP func(r *http.Request) string

	// ProtectedServices lists service instance IDs whose stop, config edit,
	// and image update need the protected scope. Restart does not.
	ProtectedServices []string

	// SyncWait is how long a real run waits for its change when the request
	// has no deadline (default 12s). With a deadline, such as the router's
	// write timeout, the wait ends shortly before it instead.
	SyncWait time.Duration
	// OpTimeout bounds a change once it is approved (default 10m). The
	// change runs on a context detached from the request, so it finishes
	// or fails on its own even when the caller stops waiting.
	OpTimeout time.Duration
}
