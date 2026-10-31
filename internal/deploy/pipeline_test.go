package deploy

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/sse"
)

func TestRunPipeline(t *testing.T) {
	t.Run("happy path stores clone and build logs", func(t *testing.T) {
		deploys := newMemDeployments()
		logs := &memLogs{}
		projects := &memProjects{items: map[string]*models.Project{}}
		envs := &memEnvironments{items: map[string]*models.Environment{}}
		containers := &memContainers{}
		gitFake := &fakeGit{progress: "cloning\n"}
		dockerFake := &fakeRuntime{}
		hub := sse.NewHub()
		go hub.Run()
		t.Cleanup(hub.Stop)

		env := &models.Environment{
			ID: "env-1", ProjectID: "proj-1", Name: "production",
			Domain: sql.NullString{String: "app.example.com", Valid: true},
			Port:   sql.NullInt32{Int32: 8080, Valid: true},
		}
		project := &models.Project{
			ID: "proj-1", RepositoryURL: "https://example.com/app.git",
			DefaultBranch: "main", DockerfilePath: "Dockerfile",
		}
		if err := project.SetBuildArgs(map[string]string{"FOO": "bar"}); err != nil {
			t.Fatal(err)
		}
		envs.items[env.ID] = env
		projects.items[project.ID] = project
		deploys.items["dep-1"] = &models.Deployment{
			ID: "dep-1", EnvironmentID: env.ID, Status: models.DeploymentStatusPending,
		}

		svc := &Service{
			deploymentRepo: deploys, logRepo: logs, projectRepo: projects,
			environmentRepo: envs, containerRepo: containers,
			gitService: gitFake, dockerClient: dockerFake, sseHub: hub,
			traefikNetwork: "traefik", buildDir: t.TempDir(),
		}

		if err := svc.RunPipeline(context.Background(), "dep-1"); err != nil {
			t.Fatalf("RunPipeline: %v", err)
		}

		select {
		case <-deploys.listed:
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup did not list deployments")
		}

		if status := deploymentStatus(t, deploys); status != models.DeploymentStatusRunning {
			t.Fatalf("status = %s", status)
		}
		if dockerFake.restartPolicy != docker.RestartUnlessStopped {
			t.Fatalf("restart policy = %s", dockerFake.restartPolicy)
		}
		if dockerFake.buildArg("FOO") != "bar" {
			t.Fatalf("build arg FOO = %q", dockerFake.buildArg("FOO"))
		}
		if !logs.contains("cloning") || !logs.contains("step 1") {
			t.Fatalf("logs = %#v", logs.messages())
		}
	})

	t.Run("build failure marks deployment failed", func(t *testing.T) {
		deploys, logs, projects, envs, svc := pipelineFixture(t)
		svc.gitService = &fakeGit{progress: "cloning\n"}
		svc.dockerClient = &fakeRuntime{buildErr: errors.New("dockerfile syntax")}

		err := svc.RunPipeline(context.Background(), "dep-1")
		if err == nil {
			t.Fatal("expected build error")
		}
		if status := deploymentStatus(t, deploys); status != models.DeploymentStatusFailed {
			t.Fatalf("status = %s", status)
		}
		if !logs.contains("dockerfile syntax") {
			t.Fatalf("logs = %#v", logs.messages())
		}
		select {
		case <-deploys.listed:
			t.Fatal("cleanup ran after a failed build")
		case <-time.After(50 * time.Millisecond):
		}
		_ = projects
		_ = envs
	})

	t.Run("domain without port fails before clone", func(t *testing.T) {
		deploys, logs, _, envs, svc := pipelineFixture(t)
		envs.items["env-1"].Domain = sql.NullString{String: "app.example.com", Valid: true}
		envs.items["env-1"].Port = sql.NullInt32{}
		gitFake := &fakeGit{}
		svc.gitService = gitFake

		err := svc.RunPipeline(context.Background(), "dep-1")
		if err == nil || !strings.Contains(err.Error(), "port") {
			t.Fatalf("error = %v", err)
		}
		if gitFake.called {
			t.Fatal("clone ran without a port")
		}
		if status := deploymentStatus(t, deploys); status != models.DeploymentStatusFailed {
			t.Fatalf("status = %s", status)
		}
		if !logs.contains("container port is required") {
			t.Fatalf("logs = %#v", logs.messages())
		}
	})

	t.Run("health check failure marks deployment failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(server.Close)
		parsed, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.ParseInt(parsed.Port(), 10, 32)
		if err != nil {
			t.Fatal(err)
		}

		deploys, logs, projects, envs, svc := pipelineFixture(t)
		envs.items["env-1"].Port = sql.NullInt32{Int32: int32(port), Valid: true}
		projects.items["proj-1"].HealthCheckPath = "/ready"
		dockerFake := &fakeRuntime{}
		svc.dockerClient = dockerFake
		svc.blueGreenDeployer = NewBlueGreenDeployer(nil, server.Client(), BlueGreenConfig{
			HealthCheckCount:    1,
			HealthCheckInterval: time.Millisecond,
			HealthCheckTimeout:  time.Second,
		})
		t.Cleanup(svc.blueGreenDeployer.Stop)

		err = svc.RunPipeline(context.Background(), "dep-1")
		if err == nil {
			t.Fatal("expected health check error")
		}
		if status := deploymentStatus(t, deploys); status != models.DeploymentStatusFailed {
			t.Fatalf("status = %s, err %v", status, err)
		}
		if !dockerFake.stopped {
			t.Fatal("container was not stopped after health check failure")
		}
		if !logs.contains("health check failed") {
			t.Fatalf("logs = %#v", logs.messages())
		}
	})
}

func TestRunPipeline_UsesBuildContext(t *testing.T) {
	_, _, projects, _, svc := pipelineFixture(t)
	projects.items["proj-1"].DockerfilePath = "docker/Dockerfile.prod"
	projects.items["proj-1"].BuildContext = "app"
	dockerFake := &fakeRuntime{}
	svc.gitService = &fakeGit{files: map[string]string{
		"app/docker/Dockerfile.prod": "FROM scratch\n",
		"app/hello.txt":              "hi\n",
	}}
	svc.dockerClient = dockerFake

	if err := svc.RunPipeline(context.Background(), "dep-1"); err != nil {
		t.Fatal(err)
	}
	dockerFake.mu.Lock()
	contextDir := dockerFake.contextDir
	dockerfile := dockerFake.dockerfile
	dockerFake.mu.Unlock()
	if !strings.HasSuffix(contextDir, string(filepath.Separator)+"app") {
		t.Fatalf("context = %s", contextDir)
	}
	if dockerfile != "docker/Dockerfile.prod" {
		t.Fatalf("dockerfile = %s", dockerfile)
	}
}

func TestRunPipeline_MissingDockerfile(t *testing.T) {
	deploys, _, projects, _, svc := pipelineFixture(t)
	projects.items["proj-1"].DockerfilePath = "nope/Dockerfile"
	svc.gitService = &fakeGit{files: map[string]string{"Dockerfile": "FROM scratch\n"}}

	err := svc.RunPipeline(context.Background(), "dep-1")
	if err == nil || !strings.Contains(err.Error(), "dockerfile not found") {
		t.Fatalf("RunPipeline err = %v", err)
	}
	if status := deploymentStatus(t, deploys); status != models.DeploymentStatusFailed {
		t.Fatalf("status = %s", status)
	}
}

func TestRunPipeline_CancelDuringBuildStaysCanceled(t *testing.T) {
	deploys, _, projects, _, svc := pipelineFixture(t)
	dockerFake := &fakeRuntime{blockBuild: true, buildStarted: make(chan struct{})}
	svc.dockerClient = dockerFake
	projects.items["proj-1"].DockerfilePath = "Dockerfile"

	ctx, cancel := context.WithCancel(context.Background())
	svc.trackRun("dep-1", cancel)
	defer svc.untrackRun("dep-1")

	done := make(chan error, 1)
	go func() {
		done <- svc.RunPipeline(ctx, "dep-1")
	}()

	select {
	case <-dockerFake.buildStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("build did not start")
	}

	if err := svc.Cancel(context.Background(), "dep-1", "canceled by user"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrCanceled) {
			t.Fatalf("RunPipeline err = %v, want ErrCanceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not stop after cancel")
	}

	if status := deploymentStatus(t, deploys); status != models.DeploymentStatusCanceled {
		t.Fatalf("status = %s", status)
	}
}

func deploymentStatus(t *testing.T, deploys *memDeployments) models.DeploymentStatus {
	t.Helper()
	got, err := deploys.GetByID(context.Background(), "dep-1")
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func pipelineFixture(t *testing.T) (*memDeployments, *memLogs, *memProjects, *memEnvironments, *Service) {
	t.Helper()
	deploys := newMemDeployments()
	logs := &memLogs{}
	projects := &memProjects{items: map[string]*models.Project{
		"proj-1": {
			ID: "proj-1", RepositoryURL: "https://example.com/app.git",
			DefaultBranch: "main", DockerfilePath: "Dockerfile",
		},
	}}
	envs := &memEnvironments{items: map[string]*models.Environment{
		"env-1": {ID: "env-1", ProjectID: "proj-1", Name: "production", Port: sql.NullInt32{Int32: 8080, Valid: true}},
	}}
	deploys.items["dep-1"] = &models.Deployment{ID: "dep-1", EnvironmentID: "env-1", Status: models.DeploymentStatusPending}
	hub := sse.NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)
	svc := &Service{
		deploymentRepo: deploys, logRepo: logs, projectRepo: projects, environmentRepo: envs,
		containerRepo: &memContainers{}, gitService: &fakeGit{progress: "cloning\n"},
		dockerClient: &fakeRuntime{}, sseHub: hub, traefikNetwork: "traefik", buildDir: t.TempDir(),
	}
	return deploys, logs, projects, envs, svc
}

type memDeployments struct {
	mu     sync.Mutex
	items  map[string]*models.Deployment
	listed chan struct{}
	once   sync.Once
}

func newMemDeployments() *memDeployments {
	return &memDeployments{items: map[string]*models.Deployment{}, listed: make(chan struct{})}
}

func (m *memDeployments) Create(_ context.Context, d *models.Deployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[d.ID] = d
	return nil
}

func (m *memDeployments) GetByID(_ context.Context, id string) (*models.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cloned := *d
	return &cloned, nil
}

func (m *memDeployments) UpdateStatus(_ context.Context, id string, status models.DeploymentStatus, errorMsg *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.items[id]
	if !ok {
		return errors.New("not found")
	}
	d.Status = status
	if errorMsg != nil {
		d.ErrorMessage = sql.NullString{String: *errorMsg, Valid: true}
	}
	return nil
}

func (m *memDeployments) ListByEnvironmentID(_ context.Context, _ string, _ int) ([]*models.Deployment, error) {
	m.once.Do(func() { close(m.listed) })
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*models.Deployment, 0, len(m.items))
	for _, d := range m.items {
		cloned := *d
		out = append(out, &cloned)
	}
	return out, nil
}

func (m *memDeployments) ListByStatus(_ context.Context, status models.DeploymentStatus) ([]*models.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.Deployment
	for _, d := range m.items {
		if d.Status == status {
			cloned := *d
			out = append(out, &cloned)
		}
	}
	return out, nil
}

type memLogs struct {
	mu    sync.Mutex
	items []*models.DeploymentLog
}

func (m *memLogs) Create(_ context.Context, entry *models.DeploymentLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.ID = uuid.NewString()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	cloned := *entry
	m.items = append(m.items, &cloned)
	return nil
}

func (m *memLogs) ListByDeploymentID(_ context.Context, deploymentID string) ([]*models.DeploymentLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.DeploymentLog
	for _, entry := range m.items {
		if entry.DeploymentID == deploymentID {
			cloned := *entry
			out = append(out, &cloned)
		}
	}
	return out, nil
}

func (m *memLogs) contains(part string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.items {
		if strings.Contains(entry.Message, part) {
			return true
		}
	}
	return false
}

func (m *memLogs) messages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.items))
	for i, entry := range m.items {
		out[i] = entry.Message
	}
	return out
}

type memProjects struct {
	items map[string]*models.Project
}

func (m *memProjects) GetByID(_ context.Context, id string) (*models.Project, error) {
	p, ok := m.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}

type memEnvironments struct {
	items map[string]*models.Environment
}

func (m *memEnvironments) GetByID(_ context.Context, id string) (*models.Environment, error) {
	env, ok := m.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return env, nil
}

type memContainers struct {
	mu    sync.Mutex
	items []*models.Container
}

func (m *memContainers) Create(_ context.Context, c *models.Container) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	cloned := *c
	m.items = append(m.items, &cloned)
	return nil
}

func (m *memContainers) ListByDeploymentID(_ context.Context, deploymentID string) ([]*models.Container, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*models.Container
	for _, c := range m.items {
		if c.DeploymentID.String == deploymentID {
			cloned := *c
			out = append(out, &cloned)
		}
	}
	return out, nil
}

func (m *memContainers) UpdateStatus(_ context.Context, id string, status models.ContainerStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.items {
		if c.ID == id {
			c.Status = status
		}
	}
	return nil
}

type fakeGit struct {
	progress string
	called   bool
	err      error
	files    map[string]string
}

func (f *fakeGit) Clone(_ context.Context, opts git.CloneOptions) error {
	f.called = true
	if opts.Path != "" {
		files := f.files
		if files == nil {
			files = map[string]string{"Dockerfile": "FROM scratch\n"}
		}
		for name, body := range files {
			full := filepath.Join(opts.Path, name)
			if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
				return err
			}
		}
	}
	if opts.Progress != nil && f.progress != "" {
		_, _ = io.WriteString(opts.Progress, f.progress)
	}
	return f.err
}

type fakeRuntime struct {
	mu            sync.Mutex
	buildErr      error
	restartPolicy string
	buildArgs     map[string]*string
	contextDir    string
	dockerfile    string
	stopped       bool
	blockBuild    bool
	buildStarted  chan struct{}
}

func (f *fakeRuntime) BuildImage(ctx context.Context, opts docker.BuildOptions, output io.Writer) error {
	f.mu.Lock()
	f.buildArgs = opts.BuildArgs
	f.contextDir = opts.ContextDir
	f.dockerfile = opts.Dockerfile
	block := f.blockBuild
	started := f.buildStarted
	f.mu.Unlock()
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	if output != nil {
		_, _ = io.WriteString(output, "step 1\n")
	}
	return f.buildErr
}

func (f *fakeRuntime) buildArg(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.buildArgs == nil || f.buildArgs[key] == nil {
		return ""
	}
	return *f.buildArgs[key]
}

func (f *fakeRuntime) CreateContainer(_ context.Context, opts docker.ContainerOptions) (string, error) {
	f.mu.Lock()
	f.restartPolicy = opts.RestartPolicy
	f.mu.Unlock()
	return "container-1", nil
}

func (f *fakeRuntime) InspectContainer(_ context.Context, _ string) (container.InspectResponse, error) {
	return container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"traefik": {IPAddress: netip.MustParseAddr("127.0.0.1")},
			},
		},
	}, nil
}

func (f *fakeRuntime) StopContainer(_ context.Context, _ string) error {
	f.mu.Lock()
	f.stopped = true
	f.mu.Unlock()
	return nil
}

func (f *fakeRuntime) RemoveContainer(_ context.Context, _ string) error {
	return nil
}

func (f *fakeRuntime) PruneImages(context.Context) (image.PruneReport, error) {
	return image.PruneReport{}, nil
}
