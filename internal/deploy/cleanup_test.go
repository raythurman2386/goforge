package deploy

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// hostRuntime models the Docker host: a container exists from create until
// remove, and runs from create until stop. It lets a test count what
// `docker ps` and `docker ps -a` would show after a redeploy.
type hostRuntime struct {
	mu      sync.Mutex
	exists  map[string]bool
	running map[string]bool
	next    string
}

func newHostRuntime(initial ...string) *hostRuntime {
	h := &hostRuntime{exists: map[string]bool{}, running: map[string]bool{}}
	for _, id := range initial {
		h.exists[id] = true
		h.running[id] = true
	}
	return h
}

func (h *hostRuntime) BuildImage(context.Context, docker.BuildOptions, io.Writer) error { return nil }

func (h *hostRuntime) CreateContainer(_ context.Context, _ docker.ContainerOptions) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.exists[h.next] = true
	h.running[h.next] = true
	return h.next, nil
}

func (h *hostRuntime) InspectContainer(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{}, nil
}

func (h *hostRuntime) StopContainer(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.exists[id] {
		return errors.New("no such container")
	}
	h.running[id] = false
	return nil
}

func (h *hostRuntime) RemoveContainer(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.exists[id] {
		return errors.New("no such container")
	}
	delete(h.exists, id)
	delete(h.running, id)
	return nil
}

func (h *hostRuntime) PruneImages(context.Context) (image.PruneReport, error) {
	return image.PruneReport{}, nil
}

// ps returns running containers; psAll returns every container on the host.
func (h *hostRuntime) ps() []string    { return h.list(true) }
func (h *hostRuntime) psAll() []string { return h.list(false) }

func (h *hostRuntime) list(runningOnly bool) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.exists))
	for id := range h.exists {
		if runningOnly && !h.running[id] {
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// redeployFixture is one environment whose previous deployment is running in
// container "old-ctr"; dep-1 is the pending redeploy that will create "new-ctr".
func redeployFixture(t *testing.T) (*Service, *memDeployments, *memContainers, *hostRuntime) {
	t.Helper()
	deploys, _, _, _, svc := pipelineFixture(t)
	deploys.items["dep-old"] = &models.Deployment{
		ID: "dep-old", EnvironmentID: "env-1", Status: models.DeploymentStatusRunning,
	}
	containers := &memContainers{}
	if err := containers.Create(context.Background(), &models.Container{
		ID:           "row-old",
		DeploymentID: sql.NullString{String: "dep-old", Valid: true},
		DockerID:     "old-ctr",
		Status:       models.ContainerStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	host := newHostRuntime("old-ctr")
	host.next = "new-ctr"
	svc.containerRepo = containers
	svc.dockerClient = host
	svc.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return svc, deploys, containers, host
}

func containerRowStatus(t *testing.T, containers *memContainers, dockerID string) models.ContainerStatus {
	t.Helper()
	containers.mu.Lock()
	defer containers.mu.Unlock()
	for _, c := range containers.items {
		if c.DockerID == dockerID {
			return c.Status
		}
	}
	t.Fatalf("no container row for %s", dockerID)
	return ""
}

// TestRedeployRetiresPreviousContainer is the regression for the ghost
// container seen after a goforge-docs redeploy: cleanup stopped the previous
// deployment's container but never removed it, so it stayed on the host.
func TestRedeployRetiresPreviousContainer(t *testing.T) {
	svc, _, containers, host := redeployFixture(t)

	if err := svc.RunPipeline(context.Background(), "dep-1"); err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}

	// Cleanup runs in the background after RunPipeline returns; wait until
	// it has finished with the old container (row marked exited).
	deadline := time.Now().Add(2 * time.Second)
	for containerRowStatus(t, containers, "old-ctr") != models.ContainerStatusExited {
		if time.Now().After(deadline) {
			t.Fatalf("cleanup did not retire old-ctr; docker ps -a = %v", host.psAll())
		}
		time.Sleep(5 * time.Millisecond)
	}

	if got := host.ps(); len(got) != 1 || got[0] != "new-ctr" {
		t.Fatalf("docker ps = %v, want only new-ctr", got)
	}
	if got := host.psAll(); len(got) != 1 || got[0] != "new-ctr" {
		t.Fatalf("docker ps -a = %v, want only new-ctr (old container left behind)", got)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusExited {
		t.Fatalf("old container row status = %s, want exited", st)
	}
	if st := containerRowStatus(t, containers, "new-ctr"); st != models.ContainerStatusRunning {
		t.Fatalf("new container row status = %s, want running", st)
	}
}

// TestCleanupOldDeploymentsRemovesWhenAlreadyStopped covers a previous
// container that exited on its own: stop fails, remove must still run.
func TestCleanupOldDeploymentsRemovesWhenAlreadyStopped(t *testing.T) {
	svc, deploys, containers, host := redeployFixture(t)
	deploys.items["dep-1"].Status = models.DeploymentStatusRunning
	svc.dockerClient = &stopFailsRuntime{hostRuntime: host}

	if err := svc.CleanupOldDeployments(context.Background(), "env-1", "dep-1"); err != nil {
		t.Fatalf("CleanupOldDeployments: %v", err)
	}
	if got := host.psAll(); len(got) != 0 {
		t.Fatalf("docker ps -a = %v, want old-ctr removed", got)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusExited {
		t.Fatalf("old container row status = %s, want exited", st)
	}
}

type stopFailsRuntime struct{ *hostRuntime }

func (s *stopFailsRuntime) StopContainer(context.Context, string) error {
	return errors.New("container already stopped")
}
