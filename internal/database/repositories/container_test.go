package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestContainerRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewContainerRepository(db)
	ctx := context.Background()

	dockerID := "docker-" + uniqueID()
	c := &models.Container{
		DockerID: dockerID,
		Name:     "web-app-" + uniqueID(),
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
	t.Parallel()
	db := getTestDB(t)

	repo := NewContainerRepository(db)
	ctx := context.Background()

	dockerID := "docker-" + uniqueID()
	c := &models.Container{
		DockerID: dockerID,
		Name:     "web-app-" + uniqueID(),
		Image:    "nginx:latest",
		Status:   models.ContainerStatusRunning,
	}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, err := repo.GetByDockerID(ctx, dockerID)
	if err != nil {
		t.Fatalf("GetByDockerID() error = %v", err)
	}

	if found.Name != c.Name {
		t.Errorf("GetByDockerID() Name = %q, want %q", found.Name, c.Name)
	}
}

func TestContainerRepository_GetByDockerID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewContainerRepository(db)
	ctx := context.Background()

	_, err := repo.GetByDockerID(ctx, "nonexistent-docker-"+uniqueID())
	if err != ErrNotFound {
		t.Errorf("GetByDockerID() error = %v, want ErrNotFound", err)
	}
}

func TestContainerRepository_ListByDeploymentID(t *testing.T) {
	t.Skip("Skipping: requires full deployment chain (user->project->environment->deployment)")
}
