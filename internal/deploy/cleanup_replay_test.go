// Replays of the counterexamples TLC found in formal/deploy-retire/DeployRetire.tla
// (findings F1 and F2), one test step per model action. They failed on the code
// before the F1/F2 fix and now guard it. They used to sit behind the
// formalreplay build tag; the tag is gone so `make test` and CI keep them
// fixed. `go test -tags formalreplay -run TestReplay ./internal/deploy/` still
// runs them.
package deploy

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func addRunningDeployment(t *testing.T, deploys *memDeployments, containers *memContainers, host *hostRuntime, id, rowID, dockerID string, createdAt time.Time) {
	t.Helper()
	deploys.mu.Lock()
	deploys.items[id] = &models.Deployment{
		ID: id, EnvironmentID: "env-1", Status: models.DeploymentStatusRunning,
		CreatedAt: createdAt, FinishedAt: sql.NullTime{Time: createdAt, Valid: true},
	}
	deploys.mu.Unlock()
	if err := containers.Create(context.Background(), &models.Container{
		ID: rowID, DeploymentID: sql.NullString{String: id, Valid: true},
		DockerID: dockerID, Status: models.ContainerStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	host.exists[dockerID] = true
	host.running[dockerID] = true
	host.mu.Unlock()
}

// F1 (NewestKeepsRunning, DeployRetireBackToBack.cfg). Two redeploys within one
// grace period: dep-1 reaches running, then dep-2 reaches running before dep-1's
// grace ends. dep-1's CleanupOldDeployments wakes; before the fix
// retireSuperseded skipped only dep-1 itself and non-running rows, so it retired
// dep-2's container too and the newest deployment went down. Now it never
// retires a deployment newer than the newest one whose grace is over (here the
// grace is 0, so that is dep-2 and dep-1's cleanup retires dep-old and dep-1).
func TestReplayBackToBackRedeployKeepsNewest(t *testing.T) {
	svc, deploys, containers, host := redeployFixture(t) // dep-old running in old-ctr
	now := time.Now()
	deploys.items["dep-old"].CreatedAt = now.Add(-time.Hour)
	delete(deploys.items, "dep-1")
	// 1. a redeploy reaches running (dep-1, mid-ctr)
	addRunningDeployment(t, deploys, containers, host, "dep-1", "row-mid", "mid-ctr", now.Add(-2*time.Minute))
	// 2. a redeploy reaches running (dep-2, newest-ctr), inside dep-1's grace
	addRunningDeployment(t, deploys, containers, host, "dep-2", "row-newest", "newest-ctr", now.Add(-time.Minute))
	// 3-7. dep-1's grace passes, its cleanup wakes (gracePeriod is 0 here, so
	// waitGrace returns at once), lists, reads rows, stops and removes.
	if err := svc.CleanupOldDeployments(context.Background(), "env-1", "dep-1"); err != nil {
		t.Fatalf("CleanupOldDeployments(dep-1): %v", err)
	}
	if st := containerRowStatus(t, containers, "newest-ctr"); st != models.ContainerStatusRunning {
		t.Errorf("newest deployment's container row = %s after dep-1's cleanup, want running", st)
	}
	if got := host.ps(); len(got) != 1 || got[0] != "newest-ctr" {
		t.Errorf("docker ps = %v after dep-1's cleanup, want only newest-ctr (the newest deployment)", got)
	}
	// dep-2's own cleanup then retires dep-1: nothing is left running.
	if err := svc.CleanupOldDeployments(context.Background(), "env-1", "dep-2"); err != nil {
		t.Fatalf("CleanupOldDeployments(dep-2): %v", err)
	}
	if got := host.ps(); len(got) != 1 || got[0] != "newest-ctr" {
		t.Errorf("docker ps = %v after both cleanups, want only newest-ctr", got)
	}
}

type dockerDownRuntime struct{ *hostRuntime }

func (d *dockerDownRuntime) StopContainer(context.Context, string) error {
	return errors.New("Cannot connect to the Docker daemon")
}

func (d *dockerDownRuntime) RemoveContainer(context.Context, string) error {
	return errors.New("Cannot connect to the Docker daemon")
}

// F2 (AfterGraceAtMostOneRunning, DeployRetireDockerErrors.cfg). StopContainer
// and RemoveContainer both return an error. Before the fix retireContainer still
// marked the row exited, so neither a later cleanup nor RecoverPendingRetires
// (hasSuperseded reads rows) ever looked at the old container again and it ran
// forever. Now the row stays running, the cleanup retries, and after a restart
// RecoverPendingRetires retires it.
func TestReplayDockerErrorsLeaveNoSecondRunning(t *testing.T) {
	svc, containers, host := restartFixture(t, 0, time.Minute) // dep-old/old-ctr, dep-1/new-ctr running
	realHost := host
	// 3-8. cleanup wakes, lists, reads the row, docker stop fails, docker rm
	// fails; the row must stay running and the cleanup keeps retrying until
	// the restart below kills it (ctx ends).
	svc.dockerClient = &dockerDownRuntime{hostRuntime: host}
	svc.retireRetry = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := svc.CleanupOldDeployments(ctx, "env-1", "dep-1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CleanupOldDeployments with Docker down = %v, want it still retrying until ctx ends", err)
	}
	if st := containerRowStatus(t, containers, "old-ctr"); st != models.ContainerStatusRunning {
		t.Errorf("old-ctr row = %s after failed retire, want running (so it is retried)", st)
	}
	// Docker is back; goforge restarts and RecoverPendingRetires runs.
	svc.dockerClient = realHost
	if err := svc.RecoverPendingRetires(context.Background()); err != nil {
		t.Fatalf("RecoverPendingRetires: %v", err)
	}
	if got := realHost.ps(); len(got) != 1 || got[0] != "new-ctr" {
		t.Errorf("docker ps = %v after cleanup and boot recovery, want only new-ctr (row old-ctr = %s)",
			got, containerRowStatus(t, containers, "old-ctr"))
	}
}
