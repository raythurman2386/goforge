package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/gocool/internal/models"
)

func TestContainerRepository_Create(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewContainerRepository(db)
	ctx := context.Background()

	c := &models.Container{
		DockerID: "docker-123",
		Name:     "web-app",
		Image:    "nginx:latest",
		Status:   models.ContainerStatusRunning,
	}

	err := repo.Create(ctx, c)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if c.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestContainerRepository_GetByDockerID(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewContainerRepository(db)
	ctx := context.Background()

	c := &models.Container{
		DockerID: "docker-123",
		Name:     "web-app",
		Image:    "nginx:latest",
		Status:   models.ContainerStatusRunning,
	}
	_ = repo.Create(ctx, c)

	found, err := repo.GetByDockerID(ctx, "docker-123")
	if err != nil {
		t.Fatalf("GetByDockerID() error = %v", err)
	}

	if found.Name != c.Name {
		t.Errorf("GetByDockerID() Name = %q, want %q", found.Name, c.Name)
	}
}

func TestContainerRepository_ListByDeploymentID(t *testing.T) {
	t.Skip("Skipping: requires full deployment chain (user->project->environment->deployment)")
}
