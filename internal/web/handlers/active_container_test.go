package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

type fakeContainers map[string][]*models.Container

func (f fakeContainers) ListByDeploymentID(_ context.Context, id string) ([]*models.Container, error) {
	if id == "boom" {
		return nil, errors.New("db down")
	}
	return f[id], nil
}

type fakeDeployments []*models.Deployment

func (f fakeDeployments) ListByEnvironmentID(context.Context, string, int) ([]*models.Deployment, error) {
	return f, nil
}

func ctr(id string, status models.ContainerStatus) *models.Container {
	return &models.Container{ID: id, DockerID: "docker-" + id, Status: status}
}

func dep(id string, status models.DeploymentStatus) *models.Deployment {
	return &models.Deployment{ID: id, Status: status}
}

func TestActiveContainer(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name        string
		containers  fakeContainers
		deployments fakeDeployments
		latest      *models.Deployment
		want        string // container ID, "" for nil
	}{
		{
			name:       "running container of latest deployment",
			containers: fakeContainers{"d2": {ctr("old", models.ContainerStatusExited), ctr("new", models.ContainerStatusRunning)}},
			latest:     dep("d2", models.DeploymentStatusRunning),
			want:       "new",
		},
		{
			name:        "deploy in flight keeps previous running container",
			containers:  fakeContainers{"d1": {ctr("prev", models.ContainerStatusRunning)}},
			deployments: fakeDeployments{dep("d2", models.DeploymentStatusBuilding), dep("d1", models.DeploymentStatusRunning)},
			latest:      dep("d2", models.DeploymentStatusBuilding),
			want:        "prev",
		},
		{
			name: "prefers newest successful deployment over older ones",
			containers: fakeContainers{
				"d1": {ctr("oldest", models.ContainerStatusRunning)},
				"d2": {ctr("newer", models.ContainerStatusRunning)},
			},
			deployments: fakeDeployments{dep("d3", models.DeploymentStatusDeploying), dep("d2", models.DeploymentStatusRunning), dep("d1", models.DeploymentStatusRunning)},
			latest:      dep("d3", models.DeploymentStatusDeploying),
			want:        "newer",
		},
		{
			name:        "skips failed deployments with leftover running rows",
			containers:  fakeContainers{"d2": {ctr("failed-run", models.ContainerStatusRunning)}, "d1": {ctr("good", models.ContainerStatusRunning)}},
			deployments: fakeDeployments{dep("d3", models.DeploymentStatusPending), dep("d2", models.DeploymentStatusFailed), dep("d1", models.DeploymentStatusRunning)},
			latest:      dep("d3", models.DeploymentStatusPending),
			want:        "good",
		},
		{
			name:        "never returns a stopped container",
			containers:  fakeContainers{"d2": {ctr("dead", models.ContainerStatusExited)}, "d1": {ctr("also-dead", models.ContainerStatusDead)}},
			deployments: fakeDeployments{dep("d2", models.DeploymentStatusRunning), dep("d1", models.DeploymentStatusRunning)},
			latest:      dep("d2", models.DeploymentStatusRunning),
			want:        "",
		},
		{
			name:   "no deployment",
			latest: nil,
			want:   "",
		},
		{
			name:       "list error is empty state",
			containers: fakeContainers{},
			latest:     dep("boom", models.DeploymentStatusRunning),
			want:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := activeContainer(context.Background(), tt.containers, tt.deployments, "env-1", tt.latest, logger)
			gotID := ""
			if got != nil {
				gotID = got.ID
				if got.Status != models.ContainerStatusRunning {
					t.Fatalf("returned non-running container %q (%s)", got.ID, got.Status)
				}
			}
			if gotID != tt.want {
				t.Fatalf("container = %q, want %q", gotID, tt.want)
			}
		})
	}
}
