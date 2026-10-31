//go:build formalreplay

// Replays of the counterexamples TLC finds in formal/deploy-retire/DeployRetire.tla,
// one test step per model action. They assert the rule the model checks, so they
// FAIL on the code as of the sync commit in
// .cursor/skills/verify-goforge-deploy-retire/SKILL.md. They are behind the
// formalreplay build tag so `make test` and CI stay green until the findings are
// fixed; run them with:
//
//	go test -tags formalreplay -run TestReplay -count=1 ./internal/deploy/
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
// grace ends. dep-1's CleanupOldDeployments wakes, retireSuperseded lists the
// environment and skips only dep-1 itself and non-running rows, so it retires
// dep-2's container too: the newest deployment goes down.
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
// and RemoveContainer both return an error; retireContainer still marks the row
// exited, so neither a later cleanup nor RecoverPendingRetires (hasSuperseded
// reads rows) ever looks at the old container again. It runs forever.
func TestReplayDockerErrorsLeaveNoSecondRunning(t *testing.T) {
	svc, containers, host := restartFixture(t, 0, time.Minute) // dep-old/old-ctr, dep-1/new-ctr running
	realHost := host
	// 3-8. cleanup wakes, lists, reads the row, docker stop fails, docker rm fails, row marked exited.
	svc.dockerClient = &dockerDownRuntime{hostRuntime: host}
	if err := svc.CleanupOldDeployments(context.Background(), "env-1", "dep-1"); err != nil {
		t.Fatalf("CleanupOldDeployments: %v", err)
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
