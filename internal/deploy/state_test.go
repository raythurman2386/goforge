package deploy

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		from models.DeploymentStatus
		to   models.DeploymentStatus
		want bool
	}{
		{models.DeploymentStatusPending, models.DeploymentStatusCloning, true},
		{models.DeploymentStatusBuilding, models.DeploymentStatusCanceled, true},
		{models.DeploymentStatusBuilding, models.DeploymentStatusFailed, true},
		{models.DeploymentStatusRunning, models.DeploymentStatusRolledBack, true},
		{models.DeploymentStatusCanceled, models.DeploymentStatusFailed, false},
		{models.DeploymentStatusCanceled, models.DeploymentStatusBuilding, false},
		{models.DeploymentStatusCanceled, models.DeploymentStatusCanceled, false},
		{models.DeploymentStatusFailed, models.DeploymentStatusCanceled, false},
		{models.DeploymentStatusRolledBack, models.DeploymentStatusFailed, false},
	}
	for _, tt := range tests {
		if got := isValidTransition(tt.from, tt.to); got != tt.want {
			t.Errorf("isValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestCancelStopsTrackedRun(t *testing.T) {
	deploys := newMemDeployments()
	deploys.items["d1"] = &models.Deployment{ID: "d1", Status: models.DeploymentStatusBuilding}
	svc := &Service{deploymentRepo: deploys}

	ctx, cancel := context.WithCancel(context.Background())
	svc.trackRun("d1", cancel)
	if err := svc.Cancel(context.Background(), "d1", "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancel did not stop the run")
	}
	got, err := deploys.GetByID(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.DeploymentStatusCanceled {
		t.Fatalf("status = %s", got.Status)
	}
	if transErr := svc.Transition(context.Background(), "d1", models.DeploymentStatusFailed, nil); transErr == nil {
		t.Fatal("canceled deployment transitioned to failed")
	}
	got, err = deploys.GetByID(context.Background(), "d1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.DeploymentStatusCanceled {
		t.Fatalf("status after failed transition = %s", got.Status)
	}
}
