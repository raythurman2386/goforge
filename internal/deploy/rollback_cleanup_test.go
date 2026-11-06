package deploy

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

type rollbackTrackingRuntime struct {
	fakeRuntime
	createdOpts docker.ContainerOptions
	removedID   string
}

func (r *rollbackTrackingRuntime) CreateContainer(_ context.Context, opts docker.ContainerOptions) (string, error) {
	r.createdOpts = opts
	return "restored-docker-id", nil
}

func (r *rollbackTrackingRuntime) RemoveContainer(_ context.Context, id string) error {
	r.removedID = id
	return nil
}

type rollbackFailingContainerStore struct{ memContainers }

func (*rollbackFailingContainerStore) Create(context.Context, *models.Container) error {
	return errors.New("insert failed")
}

func TestRestoreDeployment_UnrecordedCopyIsRemoved(t *testing.T) {
	runtime := &rollbackTrackingRuntime{}
	store := &rollbackFailingContainerStore{}
	svc := &Service{containerRepo: store, projectRepo: &memProjects{items: map[string]*models.Project{"project": {ID: "project"}}}, environmentRepo: &memEnvironments{items: map[string]*models.Environment{"env": {ID: "env", ProjectID: "project"}}}, dockerClient: runtime, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	target := &models.Deployment{ID: "target", EnvironmentID: "env"}
	store.items = []*models.Container{{ID: "original", DeploymentID: sql.NullString{String: "target", Valid: true}, Name: "app", Image: "unused"}}
	rm := NewRollbackManager(svc)
	if err := rm.restoreDeployment(context.Background(), target); err == nil {
		t.Fatal("want record failure")
	}
	if runtime.removedID != "restored-docker-id" {
		t.Fatalf("unrecorded restored container was not removed: %q", runtime.removedID)
	}
}

func TestRollbackRestoredOptions_KeepOwnershipLabels(t *testing.T) {
	svc := &Service{traefikNetwork: "traefik"}
	row := &models.Container{Name: "app", Image: "app:test", Labels: []byte(`{"goforge.managed":"true","goforge.project.id":"project-id","goforge.environment.id":"environment-id"}`)}
	opts := containerOptionsFromContainer(context.Background(), row, nil, nil, svc)
	if opts.Name != "app-restored" || opts.Labels[docker.ManagedLabelKey] != docker.ManagedLabelValue || opts.Labels[docker.ProjectLabelKey] != "project-id" || opts.Labels[docker.EnvironmentLabelKey] != "environment-id" {
		t.Fatalf("restored options = %+v", opts)
	}
}

type rollbackRemovalFailureRuntime struct {
	rollbackTrackingRuntime
	removeFails bool
}

func (r *rollbackRemovalFailureRuntime) RemoveContainer(ctx context.Context, id string) error {
	if r.removeFails {
		return errors.New("transient host removal failed")
	}
	return r.rollbackTrackingRuntime.RemoveContainer(ctx, id)
}

type creationCleanupIntents struct{ retained *models.ContainerCleanup }

func (s *creationCleanupIntents) Retain(_ context.Context, c *models.ContainerCleanup) error {
	s.retained = c
	return nil
}

func TestRestoreDeployment_FailedCompensationRetainsCleanupIntent(t *testing.T) {
	runtime := &rollbackRemovalFailureRuntime{removeFails: true}
	store := &rollbackFailingContainerStore{}
	intents := &creationCleanupIntents{}
	projects := &memProjects{items: map[string]*models.Project{"project": {ID: "project"}}}
	envs := &memEnvironments{items: map[string]*models.Environment{"env": {ID: "env", ProjectID: "project"}}}
	svc := &Service{containerRepo: store, projectRepo: projects, environmentRepo: envs, dockerClient: runtime, containerCleanup: intents, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	store.items = []*models.Container{{ID: "original", DeploymentID: sql.NullString{String: "target", Valid: true}, Name: "app", Image: "unused"}}
	err := NewRollbackManager(svc).restoreDeployment(context.Background(), &models.Deployment{ID: "target", EnvironmentID: "env"})
	if err == nil || intents.retained == nil || intents.retained.Reason != models.ContainerCleanupCreateFailure {
		t.Fatalf("error=%v retained=%+v", err, intents.retained)
	}
	if intents.retained.ProjectID.String != "project" || intents.retained.EnvironmentID.String != "env" || runtime.removedID != "" {
		t.Fatalf("cleanup intent or compensating removal wrong: %+v removed=%q", intents.retained, runtime.removedID)
	}
}
