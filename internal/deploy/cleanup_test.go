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
	// created_at orders deployments (retireSuperseded never retires a newer
	// one), so the fixture sets it like the database default would.
	now := time.Now()
	deploys.items["dep-old"] = &models.Deployment{
		ID: "dep-old", EnvironmentID: "env-1", Status: models.DeploymentStatusRunning,
		CreatedAt: now.Add(-time.Hour),
	}
	deploys.items["dep-1"].CreatedAt = now
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
	svc.retireRetry = 10 * time.Millisecond
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

// TestCleanupOldDeploymentsRetriesFailedRemove: Docker fails stop and remove
// on the first passes. The row must stay running (not exited) and the same
// cleanup must retry until old-ctr is gone (F2 in formal/deploy-retire).
func TestCleanupOldDeploymentsRetriesFailedRemove(t *testing.T) {
	svc, deploys, containers, host := redeployFixture(t)
	deploys.items["dep-1"].Status = models.DeploymentStatusRunning
	flaky := &flakyRuntime{hostRuntime: host, failures: 2}
	svc.dockerClient = flaky

	if err := svc.CleanupOldDeployments(context.Background(), "env-1", "dep-1"); err != nil {
		t.Fatalf("CleanupOldDeployments: %v", err)
	}
	if got := flaky.removeCalls(); got != 3 {
		t.Fatalf("RemoveContainer calls = %d, want 3 (2 failures, then success)", got)
	}
	if got := host.psAll(); len(got) != 0 {
		t.Fatalf("docker ps -a = %v, want old-ctr removed", got)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusExited {
		t.Fatalf("old container row status = %s, want exited", st)
	}
}

// TestCleanupOldDeploymentsKeepsRowRunningWhileDockerDown: while every remove
// fails, the row is never marked exited, so RecoverPendingRetires can find it.
func TestCleanupOldDeploymentsKeepsRowRunningWhileDockerDown(t *testing.T) {
	svc, deploys, containers, host := redeployFixture(t)
	deploys.items["dep-1"].Status = models.DeploymentStatusRunning
	svc.dockerClient = &flakyRuntime{hostRuntime: host, failures: 1 << 30}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := svc.CleanupOldDeployments(ctx, "env-1", "dep-1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CleanupOldDeployments = %v, want context.DeadlineExceeded (still retrying)", err)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusRunning {
		t.Fatalf("old container row status = %s, want running (retire not done)", st)
	}
}

// TestRetireSupersededNeverRetiresNewer: the cleanup of an older deployment
// whose successor is still inside its own grace retires only what is older
// than itself (F1 in formal/deploy-retire, with a real grace period).
func TestRetireSupersededNeverRetiresNewer(t *testing.T) {
	svc, deploys, containers, host := redeployFixture(t)
	now := time.Now()
	svc.blueGreenDeployer = &BlueGreenDeployer{config: BlueGreenConfig{GracePeriod: 5 * time.Minute}}
	deploys.items["dep-1"].Status = models.DeploymentStatusRunning
	deploys.items["dep-1"].CreatedAt = now.Add(-7 * time.Minute)
	deploys.items["dep-1"].FinishedAt = sql.NullTime{Time: now.Add(-6 * time.Minute), Valid: true}
	if err := containers.Create(context.Background(), &models.Container{
		ID: "row-new", DeploymentID: sql.NullString{String: "dep-1", Valid: true},
		DockerID: "new-ctr", Status: models.ContainerStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	host.exists["new-ctr"], host.running["new-ctr"] = true, true
	deploys.items["dep-2"] = &models.Deployment{
		ID: "dep-2", EnvironmentID: "env-1", Status: models.DeploymentStatusRunning,
		CreatedAt: now.Add(-2 * time.Minute), FinishedAt: sql.NullTime{Time: now.Add(-time.Minute), Valid: true},
	}
	if err := containers.Create(context.Background(), &models.Container{
		ID: "row-newest", DeploymentID: sql.NullString{String: "dep-2", Valid: true},
		DockerID: "newest-ctr", Status: models.ContainerStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	host.exists["newest-ctr"], host.running["newest-ctr"] = true, true

	if _, err := svc.retireSuperseded(context.Background(), "env-1", "dep-1"); err != nil {
		t.Fatalf("retireSuperseded(dep-1): %v", err)
	}
	if got := host.ps(); len(got) != 2 || got[0] != "new-ctr" || got[1] != "newest-ctr" {
		t.Fatalf("docker ps = %v, want new-ctr (serving through dep-2's grace) and newest-ctr", got)
	}
	if st := containerRowStatus(t, containers, "newest-ctr"); st != models.ContainerStatusRunning {
		t.Fatalf("newest container row status = %s, want running", st)
	}
}

type flakyRuntime struct {
	*hostRuntime
	mu       sync.Mutex
	failures int
	removes  int
}

func (f *flakyRuntime) StopContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	fail := f.removes < f.failures
	f.mu.Unlock()
	if fail {
		return errors.New("Cannot connect to the Docker daemon")
	}
	return f.hostRuntime.StopContainer(ctx, id)
}

func (f *flakyRuntime) RemoveContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	f.removes++
	fail := f.removes <= f.failures
	f.mu.Unlock()
	if fail {
		return errors.New("Cannot connect to the Docker daemon")
	}
	return f.hostRuntime.RemoveContainer(ctx, id)
}

func (f *flakyRuntime) removeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.removes
}

type stopFailsRuntime struct{ *hostRuntime }

func (s *stopFailsRuntime) StopContainer(context.Context, string) error {
	return errors.New("container already stopped")
}

// restartFixture is the state a goforge restart leaves behind mid-redeploy:
// dep-1 reached running finishedAgo ago with new-ctr, dep-old is still
// running with old-ctr, and no in-memory grace timer exists any more.
func restartFixture(t *testing.T, grace, finishedAgo time.Duration) (*Service, *memContainers, *hostRuntime) {
	t.Helper()
	svc, deploys, containers, host := redeployFixture(t)
	now := time.Now()
	deploys.items["dep-old"].CreatedAt = now.Add(-time.Hour)
	deploys.items["dep-old"].FinishedAt = sql.NullTime{Time: now.Add(-time.Hour), Valid: true}
	deploys.items["dep-1"].Status = models.DeploymentStatusRunning
	deploys.items["dep-1"].CreatedAt = now.Add(-finishedAgo - time.Minute)
	deploys.items["dep-1"].FinishedAt = sql.NullTime{Time: now.Add(-finishedAgo), Valid: true}
	if err := containers.Create(context.Background(), &models.Container{
		ID:           "row-new",
		DeploymentID: sql.NullString{String: "dep-1", Valid: true},
		DockerID:     "new-ctr",
		Status:       models.ContainerStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	host.exists["new-ctr"] = true
	host.running["new-ctr"] = true
	svc.blueGreenDeployer = &BlueGreenDeployer{config: BlueGreenConfig{GracePeriod: grace}}
	return svc, containers, host
}

// TestRecoverPendingRetiresAfterGraceElapsed is the regression for a goforge
// restart during the redeploy grace period: the grace timer died with the
// process, so old-ctr kept running beside new-ctr. On boot the grace is
// already over, so old-ctr is retired at once.
func TestRecoverPendingRetiresAfterGraceElapsed(t *testing.T) {
	svc, containers, host := restartFixture(t, 5*time.Minute, 6*time.Minute)

	if got := host.ps(); len(got) != 2 {
		t.Fatalf("precondition: docker ps = %v, want old-ctr and new-ctr", got)
	}

	start := time.Now()
	if err := svc.RecoverPendingRetires(context.Background()); err != nil {
		t.Fatalf("RecoverPendingRetires: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("recover took %v, want immediate retire after elapsed grace", elapsed)
	}
	if got := host.psAll(); len(got) != 1 || got[0] != "new-ctr" {
		t.Fatalf("docker ps -a = %v, want only new-ctr", got)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusExited {
		t.Fatalf("old container row status = %s, want exited", st)
	}
	if st := containerRowStatus(t, containers, "new-ctr"); st != models.ContainerStatusRunning {
		t.Fatalf("new container row status = %s, want running", st)
	}
}

// TestRecoverPendingRetiresHonorsRemainingGrace restarts with part of the
// grace period left: old-ctr keeps serving until it is over, then goes.
func TestRecoverPendingRetiresHonorsRemainingGrace(t *testing.T) {
	const grace = 5 * time.Minute
	const remaining = 400 * time.Millisecond
	svc, containers, host := restartFixture(t, grace, grace-remaining)

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- svc.RecoverPendingRetires(context.Background()) }()

	time.Sleep(50 * time.Millisecond)
	if got := host.ps(); len(got) != 2 {
		t.Fatalf("docker ps = %v during remaining grace, want old-ctr still running", got)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RecoverPendingRetires: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recover did not retire old-ctr after the remaining grace")
	}
	if elapsed := time.Since(start); elapsed < remaining-100*time.Millisecond {
		t.Fatalf("retired after %v, want about %v (remaining grace)", elapsed, remaining)
	}
	if got := host.psAll(); len(got) != 1 || got[0] != "new-ctr" {
		t.Fatalf("docker ps -a = %v, want only new-ctr", got)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusExited {
		t.Fatalf("old container row status = %s, want exited", st)
	}
}

// TestRecoverPendingRetiresLeavesCurrentAlone: with nothing superseded, boot
// recovery touches no container (and does not wait out any grace).
func TestRecoverPendingRetiresLeavesCurrentAlone(t *testing.T) {
	svc, containers, host := restartFixture(t, 5*time.Minute, time.Second)
	if err := containers.UpdateStatus(context.Background(), "row-old", models.ContainerStatusExited); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := svc.RecoverPendingRetires(context.Background()); err != nil {
		t.Fatalf("RecoverPendingRetires: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("recover waited %v with nothing to retire", elapsed)
	}
	if got := host.ps(); len(got) != 2 {
		t.Fatalf("docker ps = %v, want both containers untouched", got)
	}
	if st := containerRowStatus(t, containers, "new-ctr"); st != models.ContainerStatusRunning {
		t.Fatalf("new container row status = %s, want running", st)
	}
}
