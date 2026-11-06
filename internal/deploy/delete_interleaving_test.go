package deploy

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	mobycontainer "github.com/moby/moby/api/types/container"
	mobyimage "github.com/moby/moby/api/types/image"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
)

type failingRecordStore struct {
	memContainers
	fail bool
}

func (s *failingRecordStore) Create(ctx context.Context, c *models.Container) error {
	if s.fail {
		s.fail = false
		return errors.New("transient container row insert failure")
	}
	return s.memContainers.Create(ctx, c)
}

// interleaveRuntime is a mock Docker engine. The test controls Docker create
// separately from deletion's list, so scheduling does not decide coverage.
type interleaveRuntime struct {
	record        *failingRecordStore
	mu            sync.Mutex
	objects       map[string]mobycontainer.Summary
	createReached chan struct{}
	createRelease chan struct{}
	stopReached   chan struct{}
	stopRelease   chan struct{}
}

func (r *interleaveRuntime) BuildImage(context.Context, docker.BuildOptions, io.Writer) error {
	return nil
}
func (*interleaveRuntime) InspectContainer(context.Context, string) (mobycontainer.InspectResponse, error) {
	return mobycontainer.InspectResponse{}, nil
}
func (r *interleaveRuntime) CreateContainer(_ context.Context, opts docker.ContainerOptions) (string, error) {
	close(r.createReached)
	<-r.createRelease
	r.mu.Lock()
	defer r.mu.Unlock()
	r.objects["new-app"] = mobycontainer.Summary{ID: "new-app", Labels: opts.Labels}
	r.record.fail = true
	return "new-app", nil
}
func (r *interleaveRuntime) ListContainers(_ context.Context, _ bool) ([]mobycontainer.Summary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]mobycontainer.Summary, 0, len(r.objects))
	for _, object := range r.objects {
		out = append(out, object)
	}
	return out, nil
}
func (r *interleaveRuntime) StopContainer(_ context.Context, id string) error {
	if r.stopReached != nil && id == "existing" {
		close(r.stopReached)
		<-r.stopRelease
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objects[id]; !ok {
		return cerrdefs.ErrNotFound
	}
	return nil
}
func (r *interleaveRuntime) RemoveContainer(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objects[id]; !ok {
		return cerrdefs.ErrNotFound
	}
	delete(r.objects, id)
	return nil
}
func (*interleaveRuntime) PruneImages(context.Context) (mobyimage.PruneReport, error) {
	return mobyimage.PruneReport{}, nil
}
func (r *interleaveRuntime) has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.objects[id]
	return ok
}

type interleaveFixture struct {
	projects    *memProjects
	envs        *memEnvironments
	runtime     *interleaveRuntime
	deploy      *Service
	delete      *project.Service
	deployments *memDeployments
}

func newInterleaveFixture(t *testing.T) *interleaveFixture {
	lifecycle := docker.NewAppLifecycle()
	projects := &memProjects{items: map[string]*models.Project{"proj-1": {ID: "proj-1", DefaultBranch: "main", DockerfilePath: "Dockerfile", BuildContext: "."}}}
	envs := &memEnvironments{items: map[string]*models.Environment{"env-1": {ID: "env-1", ProjectID: "proj-1"}}}
	records := &failingRecordStore{memContainers: memContainers{items: []*models.Container{{DeploymentID: sql.NullString{String: "target", Valid: true}, Name: "old", Image: "unused"}}}}
	runtime := &interleaveRuntime{objects: map[string]mobycontainer.Summary{}, record: records, createReached: make(chan struct{}), createRelease: make(chan struct{})}
	deployments := newMemDeployments()
	deployments.items["current"] = &models.Deployment{ID: "current", EnvironmentID: "env-1", Status: models.DeploymentStatusRunning, CreatedAt: time.Now()}
	deployments.items["target"] = &models.Deployment{ID: "target", EnvironmentID: "env-1", Status: models.DeploymentStatusRunning}
	svc := &Service{projectRepo: projects, environmentRepo: envs, containerRepo: records, deploymentRepo: deployments, dockerClient: runtime, gitService: &fakeGit{}, buildDir: t.TempDir(), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), lifecycle: lifecycle}
	return &interleaveFixture{projects: projects, envs: envs, runtime: runtime, deploy: svc, delete: project.NewService(projects, envs, nil, nil, records, nil, nil, runtime).WithLifecycle(lifecycle), deployments: deployments}
}

func (f *interleaveFixture) operation(ctx context.Context, operation string) error {
	if operation == "deploy" {
		f.deployments.mu.Lock()
		delete(f.deployments.items, "current")
		delete(f.deployments.items, "target")
		f.deployments.mu.Unlock()
		f.deployments.items["dep-1"] = &models.Deployment{ID: "dep-1", EnvironmentID: "env-1", Status: models.DeploymentStatusPending}
		return f.deploy.RunPipeline(ctx, "dep-1")
	}
	_, err := NewRollbackManager(f.deploy).Rollback(ctx, "env-1", "interleaving probe")
	return err
}
func (f *interleaveFixture) remove(ctx context.Context, parent string) error {
	if parent == "project" {
		return f.delete.DeleteProject(ctx, "proj-1")
	}
	return f.delete.DeleteEnvironment(ctx, "env-1")
}
func awaitTest(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not settle")
		return nil
	}
}

func TestCreateRecordFailsBeforeDeleteList_CleansOwnHostObject(t *testing.T) {
	for _, parent := range []string{"project", "environment"} {
		for _, operation := range []string{"deploy", "rollback"} {
			t.Run(parent+"/"+operation, func(t *testing.T) {
				f := newInterleaveFixture(t)
				done := make(chan error, 1)
				go func() { done <- f.operation(context.Background(), operation) }()
				select {
				case <-f.runtime.createReached:
				case <-time.After(5 * time.Second):
					t.Fatal("no create")
				}
				// Begin delete only after the create genuinely exists in mock Docker;
				// it waits on the lifecycle while the DB failure cleanup runs.
				close(f.runtime.createRelease)
				deleteDone := make(chan error, 1)
				go func() { deleteDone <- f.remove(context.Background(), parent) }()
				if err := awaitTest(t, done); err == nil {
					t.Fatal("DB row failure should fail operation")
				}
				if err := awaitTest(t, deleteDone); err != nil {
					t.Fatal(err)
				}
				if f.runtime.has("new-app") {
					t.Fatal("new labeled host survives delete")
				}
			})
		}
	}
}

// TestDeleteListBeforeCreateFailedRecordCannotLeak pauses delete after its
// Docker enumeration while it removes an existing object. The real deploy or
// rollback operation tries to create during that interval. Shared exclusion
// prevents physical creation, then fresh parent checks reject recreation if
// delete failed; a successful project deletion stays closed to new creates.
// Every phase records a container-row insert failure where creation can occur.

func TestDeleteListBeforeCreateFailedRecordCannotLeak(t *testing.T) {
	for _, parent := range []string{"project", "environment"} {
		for _, operation := range []string{"deploy", "rollback"} {
			t.Run(parent+"/"+operation, func(t *testing.T) {
				f := newInterleaveFixture(t)
				f.runtime.objects["existing"] = mobycontainer.Summary{ID: "existing", Labels: map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue, docker.ProjectLabelKey: "proj-1", docker.EnvironmentLabelKey: "env-1"}}
				f.runtime.stopReached = make(chan struct{})
				f.runtime.stopRelease = make(chan struct{})
				// Parent remove enters its lifecycle, lists existing object and
				// blocks stop. Concurrent operation then must wait until that
				// delete finishes. This is the required list -> create ordering;
				// successful parent deletion or failed parent's re-check rejects.
				deleteDone := make(chan error, 1)
				go func() { deleteDone <- f.remove(context.Background(), parent) }()
				<-f.runtime.stopReached
				createDone := make(chan error, 1)
				go func() { createDone <- f.operation(context.Background(), operation) }()
				select {
				case <-f.runtime.createReached:
					t.Fatal("Docker create ran during parent delete/list exclusion")
				case <-time.After(50 * time.Millisecond):
				}
				close(f.runtime.stopRelease)
				if err := awaitTest(t, deleteDone); err != nil {
					t.Fatal(err)
				}
				if err := awaitTest(t, createDone); err == nil && parent == "project" {
					t.Fatal("create survives deleted parent")
				}
				if f.runtime.has("new-app") {
					t.Fatal("new app survived create-after-list")
				}
			})
		}
	}
}
