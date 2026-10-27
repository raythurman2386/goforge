package repositories

import (
	"context"
	"database/sql"
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

func TestContainerRepository_OwnedByUser(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()

	userRepo := NewUserRepository(db, nil)
	owner := &models.User{Email: uniqueEmail("container-owner")}
	other := &models.User{Email: uniqueEmail("container-other")}
	if err := userRepo.Create(ctx, owner); err != nil {
		t.Fatalf("Create(owner) error = %v", err)
	}
	if err := userRepo.Create(ctx, other); err != nil {
		t.Fatalf("Create(other) error = %v", err)
	}

	project := &models.Project{
		UserID:         owner.ID,
		Name:           "owned-" + uniqueID(),
		RepositoryURL:  "https://example.com/app.git",
		DefaultBranch:  "main",
		DockerfilePath: "Dockerfile",
		BuildContext:   ".",
	}
	if err := NewProjectRepository(db).Create(ctx, project); err != nil {
		t.Fatalf("Create(project) error = %v", err)
	}
	env := &models.Environment{ProjectID: project.ID, Name: "production", Replicas: 1}
	if err := NewEnvironmentRepository(db).Create(ctx, env); err != nil {
		t.Fatalf("Create(env) error = %v", err)
	}
	deployment := &models.Deployment{
		EnvironmentID: env.ID,
		Status:        models.DeploymentStatusPending,
		TriggerType:   models.TriggerTypeManual,
	}
	if err := NewDeploymentRepository(db).Create(ctx, deployment); err != nil {
		t.Fatalf("Create(deployment) error = %v", err)
	}

	repo := NewContainerRepository(db)
	dockerID := "docker-" + uniqueID()
	container := &models.Container{
		DeploymentID: sql.NullString{String: deployment.ID, Valid: true},
		DockerID:     dockerID,
		Name:         "web-" + uniqueID(),
		Image:        "nginx:latest",
		Status:       models.ContainerStatusRunning,
	}
	if err := repo.Create(ctx, container); err != nil {
		t.Fatalf("Create(container) error = %v", err)
	}

	owned, err := repo.OwnedByUser(ctx, dockerID, owner.ID)
	if err != nil {
		t.Fatalf("OwnedByUser(owner) error = %v", err)
	}
	if !owned {
		t.Fatal("owner does not own the container")
	}

	owned, err = repo.OwnedByUser(ctx, dockerID, other.ID)
	if err != nil {
		t.Fatalf("OwnedByUser(other) error = %v", err)
	}
	if owned {
		t.Fatal("other user owns the container")
	}

	owned, err = repo.OwnedByUser(ctx, "missing-"+uniqueID(), owner.ID)
	if err != nil {
		t.Fatalf("OwnedByUser(missing) error = %v", err)
	}
	if owned {
		t.Fatal("missing container reported as owned")
	}
}
