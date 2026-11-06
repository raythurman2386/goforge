package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	mobycontainer "github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

type cleanupProjectStore struct {
	project *models.Project
	deleted bool
}

func (s *cleanupProjectStore) Create(_ context.Context, p *models.Project) error {
	s.project = p
	return nil
}
func (s *cleanupProjectStore) GetByID(_ context.Context, id string) (*models.Project, error) {
	if s.project == nil || s.project.ID != id {
		return nil, repositories.ErrNotFound
	}
	return s.project, nil
}
func (s *cleanupProjectStore) Delete(_ context.Context, id string) error {
	if s.project == nil || s.project.ID != id {
		return repositories.ErrNotFound
	}
	s.deleted = true
	s.project = nil
	return nil
}
func (*cleanupProjectStore) Update(context.Context, *models.Project) error { return nil }
func (*cleanupProjectStore) ListByUserID(context.Context, string) ([]*models.Project, error) {
	return nil, nil
}

type cleanupEnvironmentStore struct {
	env     *models.Environment
	deleted bool
}

func (*cleanupEnvironmentStore) Create(context.Context, *models.Environment) error { return nil }
func (s *cleanupEnvironmentStore) GetByID(_ context.Context, id string) (*models.Environment, error) {
	if s.env == nil || s.env.ID != id {
		return nil, repositories.ErrNotFound
	}
	return s.env, nil
}
func (*cleanupEnvironmentStore) Update(context.Context, *models.Environment) error { return nil }
func (s *cleanupEnvironmentStore) Delete(_ context.Context, id string) error {
	if s.env == nil || s.env.ID != id {
		return repositories.ErrNotFound
	}
	s.deleted = true
	s.env = nil
	return nil
}
func (*cleanupEnvironmentStore) ListByProjectID(context.Context, string) ([]*models.Environment, error) {
	return nil, nil
}

type cleanupContainerStore struct {
	deleted []string
	pending []*models.ContainerCleanup
	err     error
}

func (s *cleanupContainerStore) DeleteByDockerID(_ context.Context, id string) error {
	if s.err != nil {
		return s.err
	}
	s.deleted = append(s.deleted, id)
	return nil
}

type cleanupRuntime struct {
	mu         sync.Mutex
	containers []mobycontainer.Summary
	stopped    []string
	removed    []string
	stopErr    error
	listErr    error
	removeErr  error
	removedCh  chan string
}

func (r *cleanupRuntime) ListContainers(context.Context, bool) ([]mobycontainer.Summary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mobycontainer.Summary(nil), r.containers...), r.listErr
}
func (r *cleanupRuntime) StopContainer(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, id)
	return r.stopErr
}
func (r *cleanupRuntime) RemoveContainer(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.removeErr != nil {
		return r.removeErr
	}
	found := false
	for _, c := range r.containers {
		if c.ID == id {
			found = true
			break
		}
	}
	if !found {
		return cerrdefs.ErrNotFound
	}
	r.removed = append(r.removed, id)
	if r.removedCh != nil {
		r.removedCh <- id
	}
	for i, c := range r.containers {
		if c.ID == id {
			copy(r.containers[i:], r.containers[i+1:])
			r.containers = r.containers[:len(r.containers)-1]
			break
		}
	}
	return nil
}

type cleanupAuditStore struct{ rows []*models.AuditLog }

func (s *cleanupAuditStore) Create(_ context.Context, row *models.AuditLog) error {
	s.rows = append(s.rows, row)
	return nil
}

func (s *cleanupContainerStore) Add(_ context.Context, c *models.ContainerCleanup) error {
	for _, existing := range s.pending {
		if existing.DockerID == c.DockerID {
			return nil
		}
	}
	s.pending = append(s.pending, c)
	return nil
}
func (s *cleanupContainerStore) List(_ context.Context) ([]*models.ContainerCleanup, error) {
	return append([]*models.ContainerCleanup(nil), s.pending...), nil
}
func (s *cleanupContainerStore) ListForParent(_ context.Context, column, id string) ([]*models.ContainerCleanup, error) {
	var out []*models.ContainerCleanup
	for _, c := range s.pending {
		if column == docker.ProjectLabelKey && c.ProjectID.String == id || column == docker.EnvironmentLabelKey && c.EnvironmentID.String == id {
			out = append(out, c)
		}
	}
	return out, nil
}
func (s *cleanupContainerStore) Complete(_ context.Context, id string) error {
	for i, c := range s.pending {
		if c.DockerID == id {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			break
		}
	}
	return nil
}

func managedAppContainer(id, projectID, envID string) mobycontainer.Summary {
	return mobycontainer.Summary{ID: id, Labels: map[string]string{
		docker.ManagedLabelKey: docker.ManagedLabelValue,
		docker.ProjectLabelKey: projectID, docker.EnvironmentLabelKey: envID,
	}}
}

func TestDeleteProject_RemovesRestoredAndRowsBeforeProject(t *testing.T) {
	projectID, envID := uuid.NewString(), uuid.NewString()
	otherProject, otherEnv := uuid.NewString(), uuid.NewString()
	projects := &cleanupProjectStore{project: &models.Project{ID: projectID}}
	envs := &cleanupEnvironmentStore{env: &models.Environment{ID: envID, ProjectID: projectID}}
	rows := &cleanupContainerStore{}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{
		managedAppContainer("original", projectID, envID),
		managedAppContainer("restored", projectID, envID),
		managedAppContainer("other", otherProject, otherEnv),
		{ID: "service", Labels: map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue}},
	}}
	svc := &Service{projectRepo: projects, environmentRepo: envs, containerRepo: rows, dockerClient: runtime}
	if err := svc.DeleteProject(context.Background(), projectID); err != nil {
		t.Fatal(err)
	}
	if !projects.deleted {
		t.Fatal("project row was not deleted")
	}
	want := []string{"original", "restored"}
	for i, id := range want {
		if runtime.stopped[i] != id || runtime.removed[i] != id || rows.deleted[i] != id {
			t.Fatalf("cleanup[%d] = stop %q remove %q row %q", i, runtime.stopped[i], runtime.removed[i], rows.deleted[i])
		}
	}
	if len(runtime.containers) != 2 {
		t.Fatalf("remaining containers = %+v", runtime.containers)
	}
}

func TestDeleteEnvironment_RemovesOnlyThatEnvironment(t *testing.T) {
	projectID, envID := uuid.NewString(), uuid.NewString()
	projects := &cleanupProjectStore{project: &models.Project{ID: projectID}}
	envs := &cleanupEnvironmentStore{env: &models.Environment{ID: envID, ProjectID: projectID}}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{
		managedAppContainer("restored", projectID, envID),
		managedAppContainer("other", projectID, uuid.NewString()),
	}}
	svc := &Service{projectRepo: projects, environmentRepo: envs, dockerClient: runtime}
	if err := svc.DeleteEnvironment(context.Background(), envID); err != nil {
		t.Fatal(err)
	}
	if !envs.deleted || len(runtime.removed) != 1 || runtime.removed[0] != "restored" {
		t.Fatalf("deleted=%v removed=%v", envs.deleted, runtime.removed)
	}
}

func TestDeleteProject_DockerFailureKeepsProject(t *testing.T) {
	projectID := uuid.NewString()
	projects := &cleanupProjectStore{project: &models.Project{ID: projectID}}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("app", projectID, uuid.NewString())}, stopErr: errors.New("daemon unavailable")}
	svc := &Service{projectRepo: projects, environmentRepo: &cleanupEnvironmentStore{}, dockerClient: runtime}
	if err := svc.DeleteProject(context.Background(), projectID); err == nil {
		t.Fatal("want cleanup error")
	}
	if projects.deleted || len(runtime.removed) != 0 {
		t.Fatal("project or container was removed after stop failed")
	}
}

func TestReconcileOrphans_RemovesMissingParentsAndAudits(t *testing.T) {
	missingProject, projectID := uuid.NewString(), uuid.NewString()
	envID, missingEnv := uuid.NewString(), uuid.NewString()
	projects := &cleanupProjectStore{project: &models.Project{ID: projectID}}
	envs := &cleanupEnvironmentStore{env: &models.Environment{ID: envID, ProjectID: projectID}}
	audit := &cleanupAuditStore{}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{
		managedAppContainer("project-orphan", missingProject, missingEnv),
		managedAppContainer("env-orphan", projectID, missingEnv),
		managedAppContainer("live", projectID, envID),
		{ID: "service", Labels: map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue}},
	}}
	svc := &Service{projectRepo: projects, environmentRepo: envs, dockerClient: runtime, audit: audit}
	if err := svc.ReconcileOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runtime.removed) != 2 {
		t.Fatalf("removed = %v", runtime.removed)
	}
	if len(audit.rows) != 2 {
		t.Fatalf("audit rows = %d", len(audit.rows))
	}
	for i, row := range audit.rows {
		if row.Action != models.AuditActionContainerOrphanRemove {
			t.Fatalf("action = %q", row.Action)
		}
		var details map[string]interface{}
		if err := json.Unmarshal(row.Details, &details); err != nil {
			t.Fatal(err)
		}
		if details["resource_ref"] != runtime.removed[i] || details["via"] != models.AuditViaSystem {
			t.Fatalf("details = %v", details)
		}
	}
}

func TestReconcileOrphans_LabelsAuthorizeRemovalWithoutDatabaseRow(t *testing.T) {
	projects := &cleanupProjectStore{}
	envs := &cleanupEnvironmentStore{}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("restored", uuid.NewString(), uuid.NewString())}}
	svc := &Service{projectRepo: projects, environmentRepo: envs, dockerClient: runtime}
	if err := svc.ReconcileOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runtime.removed) != 1 {
		t.Fatalf("restored copy was not removed: %v", runtime.removed)
	}
}

func TestRunOrphanReconcile_StartupAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	projects := &cleanupProjectStore{}
	envs := &cleanupEnvironmentStore{}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("orphan", uuid.NewString(), uuid.NewString())}, removedCh: make(chan string, 1)}
	svc := &Service{projectRepo: projects, environmentRepo: envs, dockerClient: runtime}
	done := make(chan struct{})
	go func() { svc.RunOrphanReconcile(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); close(done) }()
	select {
	case id := <-runtime.removedCh:
		if id != "orphan" {
			t.Fatalf("startup removed %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("startup pass did not remove orphan")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconcile did not stop")
	}
}

func TestDeleteProject_RowFailureIsRetriedWithoutDockerObjectOrOrphanAudit(t *testing.T) {
	projectID, envID := uuid.NewString(), uuid.NewString()
	projects := &cleanupProjectStore{project: &models.Project{ID: projectID}}
	rows := &cleanupContainerStore{err: errors.New("transient row failure")}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("app", projectID, envID)}}
	svc := &Service{projectRepo: projects, environmentRepo: &cleanupEnvironmentStore{}, containerRepo: rows, dockerClient: runtime, cleanupStore: rows}
	if err := svc.DeleteProject(context.Background(), projectID); err == nil {
		t.Fatal("first delete should retain the parent")
	}
	if projects.deleted || len(runtime.removed) != 1 || len(rows.pending) != 1 {
		t.Fatalf("deleted=%v removed=%v pending=%v", projects.deleted, runtime.removed, rows.pending)
	}
	rows.err = nil
	if err := svc.DeleteProject(context.Background(), projectID); err != nil {
		t.Fatal(err)
	}
	if !projects.deleted || len(rows.deleted) != 1 || len(rows.pending) != 0 {
		t.Fatalf("deleted=%v rows=%v pending=%v", projects.deleted, rows.deleted, rows.pending)
	}
}

func TestDeleteEnvironment_ContainerRowFailureIsRetried(t *testing.T) {
	projectID, envID := uuid.NewString(), uuid.NewString()
	envs := &cleanupEnvironmentStore{env: &models.Environment{ID: envID, ProjectID: projectID}}
	rows := &cleanupContainerStore{err: errors.New("transient")}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("app", projectID, envID)}}
	svc := &Service{projectRepo: &cleanupProjectStore{}, environmentRepo: envs, containerRepo: rows, dockerClient: runtime, cleanupStore: rows}
	if err := svc.DeleteEnvironment(context.Background(), envID); err == nil || envs.deleted {
		t.Fatalf("first delete = %v, deleted=%v", err, envs.deleted)
	}
	rows.err = nil
	if err := svc.DeleteEnvironment(context.Background(), envID); err != nil || !envs.deleted {
		t.Fatalf("retry = %v deleted=%v", err, envs.deleted)
	}
}

func TestReconcileOrphans_TransientLookupsListAndRemoveAreSafe(t *testing.T) {
	t.Run("project lookup", func(t *testing.T) {
		svc := &Service{projectRepo: &errorProjectStore{}, environmentRepo: &cleanupEnvironmentStore{}, dockerClient: &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("app", uuid.NewString(), uuid.NewString())}}}
		if err := svc.ReconcileOrphans(context.Background()); err == nil || len(svc.dockerClient.(*cleanupRuntime).removed) != 0 {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("environment lookup", func(t *testing.T) {
		id := uuid.NewString()
		svc := &Service{projectRepo: &cleanupProjectStore{project: &models.Project{ID: id}}, environmentRepo: &errorEnvironmentStore{}, dockerClient: &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("app", id, uuid.NewString())}}}
		if err := svc.ReconcileOrphans(context.Background()); err == nil || len(svc.dockerClient.(*cleanupRuntime).removed) != 0 {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("list error", func(t *testing.T) {
		runtime := &cleanupRuntime{listErr: errors.New("docker down")}
		svc := &Service{projectRepo: &cleanupProjectStore{}, environmentRepo: &cleanupEnvironmentStore{}, dockerClient: runtime}
		if err := svc.ReconcileOrphans(context.Background()); err == nil || len(runtime.removed) != 0 {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("failed remove no audit retry succeeds once", func(t *testing.T) {
		runtime := &cleanupRuntime{containers: []mobycontainer.Summary{managedAppContainer("app", uuid.NewString(), uuid.NewString())}, removeErr: errors.New("remove failed")}
		audit := &cleanupAuditStore{}
		svc := &Service{projectRepo: &cleanupProjectStore{}, environmentRepo: &cleanupEnvironmentStore{}, dockerClient: runtime, audit: audit, cleanupStore: &cleanupContainerStore{}, containerRepo: &cleanupContainerStore{}}
		if err := svc.ReconcileOrphans(context.Background()); err == nil || len(audit.rows) != 0 {
			t.Fatalf("err=%v audits=%d", err, len(audit.rows))
		}
		runtime.removeErr = nil
		if err := svc.ReconcileOrphans(context.Background()); err != nil || len(audit.rows) != 1 {
			t.Fatalf("retry=%v audits=%d", err, len(audit.rows))
		}
	})
}

type errorProjectStore struct{ cleanupProjectStore }

func (*errorProjectStore) GetByID(context.Context, string) (*models.Project, error) {
	return nil, errors.New("transient")
}

type errorEnvironmentStore struct{ cleanupEnvironmentStore }

func (*errorEnvironmentStore) GetByID(context.Context, string) (*models.Environment, error) {
	return nil, errors.New("transient")
}

func TestReconcileOrphans_ManualDeleteIntentIsNotReclassified(t *testing.T) {
	projectID, envID := uuid.NewString(), uuid.NewString()
	rows := &cleanupContainerStore{pending: []*models.ContainerCleanup{{DockerID: "gone", ProjectID: sql.NullString{String: projectID, Valid: true}, EnvironmentID: sql.NullString{String: envID, Valid: true}, Reason: models.ContainerCleanupDelete}}}
	audit := &cleanupAuditStore{}
	svc := &Service{projectRepo: &cleanupProjectStore{}, environmentRepo: &cleanupEnvironmentStore{}, containerRepo: rows, cleanupStore: rows, dockerClient: &cleanupRuntime{}, audit: audit}
	if err := svc.ReconcileOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(audit.rows) != 0 || len(rows.deleted) != 1 || len(rows.pending) != 0 {
		t.Fatalf("audit=%d deleted=%v pending=%v", len(audit.rows), rows.deleted, rows.pending)
	}
}

func TestReconcileOrphans_RetainedLiveDeleteRetryKeepsSiblings(t *testing.T) {
	projectID, envID, otherEnv := uuid.NewString(), uuid.NewString(), uuid.NewString()
	rows := &cleanupContainerStore{pending: []*models.ContainerCleanup{{DockerID: "removed", ProjectID: sql.NullString{String: projectID, Valid: true}, EnvironmentID: sql.NullString{String: envID, Valid: true}, Reason: models.ContainerCleanupDelete}}}
	projects := &mapProjectStore{cleanupProjectStore: cleanupProjectStore{project: &models.Project{ID: projectID, UserID: "owner"}}, others: map[string]*models.Project{otherEnv: {ID: otherEnv, UserID: "other"}}}
	envs := &mapEnvironmentStore{cleanupEnvironmentStore: cleanupEnvironmentStore{env: &models.Environment{ID: envID, ProjectID: projectID}}, others: map[string]*models.Environment{otherEnv: {ID: otherEnv, ProjectID: otherEnv}}}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{
		managedAppContainer("sibling", otherEnv, otherEnv),
		managedAppContainer("service", "", ""),
	}}
	audit := &cleanupAuditStore{}
	svc := &Service{projectRepo: projects, environmentRepo: envs, dockerClient: runtime, cleanupStore: rows, containerRepo: rows, audit: audit}
	if err := svc.ReconcileOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runtime.removed) != 0 || len(rows.deleted) != 1 || len(audit.rows) != 0 {
		t.Fatalf("removed=%v rows=%v audits=%v", runtime.removed, rows.deleted, audit.rows)
	}
}

type mapProjectStore struct {
	cleanupProjectStore
	others map[string]*models.Project
}

func (s mapProjectStore) GetByID(ctx context.Context, id string) (*models.Project, error) {
	if p := s.others[id]; p != nil {
		return p, nil
	}
	return s.cleanupProjectStore.GetByID(ctx, id)
}

type mapEnvironmentStore struct {
	cleanupEnvironmentStore
	others map[string]*models.Environment
}

func (s mapEnvironmentStore) GetByID(ctx context.Context, id string) (*models.Environment, error) {
	if env := s.others[id]; env != nil {
		return env, nil
	}
	return s.cleanupEnvironmentStore.GetByID(ctx, id)
}

func TestDeleteProject_DockerGoneThenRowFailureRetriesAndSkipsAudit(t *testing.T) {
	id := uuid.NewString()
	rows := &cleanupContainerStore{pending: []*models.ContainerCleanup{{DockerID: "gone", ProjectID: sql.NullString{String: id, Valid: true}, Reason: models.ContainerCleanupDelete}}}
	rows.err = errors.New("transient row deletion")
	projects := &cleanupProjectStore{project: &models.Project{ID: id}}
	audit := &cleanupAuditStore{}
	runtime := &cleanupRuntime{containers: []mobycontainer.Summary{{ID: "unrelated", Labels: map[string]string{docker.ManagedLabelKey: docker.ManagedLabelValue}}}}
	svc := &Service{projectRepo: projects, environmentRepo: &cleanupEnvironmentStore{}, dockerClient: runtime, containerRepo: rows, cleanupStore: rows, audit: audit}
	if err := svc.DeleteProject(context.Background(), id); err == nil || projects.deleted {
		t.Fatal("first row removal failure must retain parent")
	}
	rows.err = nil
	if err := svc.DeleteProject(context.Background(), id); err != nil || !projects.deleted {
		t.Fatalf("retry = %v deleted=%v", err, projects.deleted)
	}
	if len(rows.deleted) != 1 || len(rows.pending) != 0 || len(audit.rows) != 0 || len(runtime.containers) != 1 {
		t.Fatalf("rows = %v pending=%v audit=%v runtime=%v", rows.deleted, rows.pending, audit.rows, runtime.containers)
	}
}
