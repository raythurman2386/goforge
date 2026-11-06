//go:build integration

package project

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestIntegration_DeleteProjectRemovesRestoredAndDBRows(t *testing.T) {
	fixture := newCleanupIntegrationFixture(t)
	projectID := fixture.project.ID
	envID := fixture.environment.ID
	ids := fixture.createPair("project")
	deployment := &models.Deployment{EnvironmentID: envID, Status: models.DeploymentStatusRunning, TriggerType: models.TriggerTypeManual}
	if err := fixture.deployments.Create(fixture.ctx, deployment); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := fixture.containers.Create(fixture.ctx, &models.Container{DeploymentID: sql.NullString{String: deployment.ID, Valid: true}, DockerID: id, Name: "gfcleanup-it", Image: "busybox:stable", Status: models.ContainerStatusRunning}); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.service.DeleteProject(fixture.ctx, projectID); err != nil {
		t.Fatal(err)
	}
	fixture.assertRemoved(ids)
	if _, err := fixture.projects.GetByID(fixture.ctx, projectID); !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("project lookup = %v", err)
	}
}

func TestIntegration_DeleteEnvironmentRemovesRestored(t *testing.T) {
	fixture := newCleanupIntegrationFixture(t)
	ids := fixture.createPair("environment")
	if err := fixture.service.DeleteEnvironment(fixture.ctx, fixture.environment.ID); err != nil {
		t.Fatal(err)
	}
	fixture.assertRemoved(ids)
	if _, err := fixture.projects.GetByID(fixture.ctx, fixture.project.ID); err != nil {
		t.Fatalf("parent project was deleted: %v", err)
	}
	if err := fixture.service.DeleteProject(fixture.ctx, fixture.project.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIntegration_OrphanReconcileRemovesAndAudits(t *testing.T) {
	fixture := newCleanupIntegrationFixture(t)
	ids := fixture.createOrphans()
	audits := repositories.NewAuditLogRepository(fixture.db.DB)
	fixture.service.WithAudit(audits)
	if err := fixture.service.ReconcileOrphans(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	fixture.assertRemoved(ids)
	rows, err := audits.List(fixture.ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, row := range rows {
		if row.Action == models.AuditActionContainerOrphanRemove {
			found++
		}
	}
	if found != len(ids) {
		t.Fatalf("orphan audit rows = %d, want %d", found, len(ids))
	}
	if err := fixture.service.DeleteProject(fixture.ctx, fixture.project.ID); err != nil {
		t.Fatal(err)
	}
}

type cleanupIntegrationFixture struct {
	ctx         context.Context
	t           *testing.T
	docker      *docker.Client
	db          *database.DB
	service     *Service
	project     *models.Project
	environment *models.Environment
	projects    *repositories.ProjectRepository
	deployments *repositories.DeploymentRepository
	containers  *repositories.ContainerRepository
	created     []string
}

func newCleanupIntegrationFixture(t *testing.T) *cleanupIntegrationFixture {
	t.Helper()
	dsn := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GOFORGE_TEST_DATABASE_URL to a disposable database")
	}
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("disposable database unavailable: %v", err)
	}
	if err := database.MigrateUp(db, cfg); err != nil {
		db.Close()
		t.Fatal(err)
	}
	dc, err := docker.NewClient("")
	if err != nil {
		db.Close()
		t.Skipf("docker unavailable: %v", err)
	}
	f := &cleanupIntegrationFixture{ctx: context.Background(), t: t, docker: dc, db: db}
	if err := dc.Ping(f.ctx); err != nil {
		dc.Close()
		db.Close()
		t.Skipf("docker unavailable: %v", err)
	}
	f.projects = repositories.NewProjectRepository(db.DB)
	f.deployments = repositories.NewDeploymentRepository(db.DB)
	f.containers = repositories.NewContainerRepository(db.DB)
	envs := repositories.NewEnvironmentRepository(db.DB)
	users := repositories.NewUserRepository(db.DB, nil)
	user := &models.User{Email: "gfcleanup-it-" + uuid.NewString() + "@example.test"}
	if err := users.Create(f.ctx, user); err != nil {
		t.Fatal(err)
	}
	f.service = NewService(f.projects, envs, repositories.NewEnvVariableRepository(db.DB, nil), f.deployments, f.containers, nil, nil, dc)
	f.project, err = f.service.CreateProject(f.ctx, &models.Project{UserID: user.ID, Name: "gfcleanup-it-" + uuid.NewString()[:8], RepositoryURL: "https://example.invalid/app.git", DefaultBranch: "main", DockerfilePath: "Dockerfile", BuildContext: "."})
	if err != nil {
		t.Fatal(err)
	}
	list, err := envs.ListByProjectID(f.ctx, f.project.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("environments = %v, %v", list, err)
	}
	f.environment = list[0]
	t.Cleanup(func() { f.cleanup() })
	return f
}

func (f *cleanupIntegrationFixture) createPair(name string) []string {
	labels := map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue, docker.ProjectLabelKey: f.project.ID, docker.EnvironmentLabelKey: f.environment.ID}
	return f.create(name, labels, "", "-restored")
}

func (f *cleanupIntegrationFixture) createOrphans() []string {
	projectID, envID := uuid.NewString(), uuid.NewString()
	labels := map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue, docker.ProjectLabelKey: projectID, docker.EnvironmentLabelKey: envID}
	return f.create("orphan", labels, "")
}

func (f *cleanupIntegrationFixture) create(prefix string, labels map[string]string, suffixes ...string) []string {
	ids := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		id, err := f.docker.CreateContainer(f.ctx, docker.ContainerOptions{Name: "gfcleanup-it-" + prefix + "-" + uuid.NewString()[:8] + suffix, Image: "busybox:stable", Cmd: []string{"sleep", "300"}, Labels: labels})
		if err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, id)
		f.created = append(f.created, id)
	}
	return ids
}

func (f *cleanupIntegrationFixture) assertRemoved(ids []string) {
	for _, id := range ids {
		if _, err := f.docker.InspectContainer(f.ctx, id); !docker.IsNotFound(err) {
			f.t.Errorf("container %s still exists: %v", id, err)
		}
		if _, err := f.containers.GetByDockerID(f.ctx, id); !errors.Is(err, repositories.ErrNotFound) {
			f.t.Errorf("container row %s remains or lookup failed: %v", id, err)
		}
	}

}

func (f *cleanupIntegrationFixture) cleanup() {
	for _, id := range f.created {
		_ = f.docker.RemoveContainer(context.Background(), id)
	}
	_ = f.service.DeleteProject(context.Background(), f.project.ID)
	_ = f.docker.Close()
	_ = f.db.Close()
}
