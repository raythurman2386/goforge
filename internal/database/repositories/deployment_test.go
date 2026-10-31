package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestDeploymentRepository_ListByStatus(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("deploy-status")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("Create(user) error = %v", err)
	}
	project := &models.Project{
		UserID:         user.ID,
		Name:           "deploy-status-" + uniqueID(),
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

	repo := NewDeploymentRepository(db)
	running := &models.Deployment{EnvironmentID: env.ID, Status: models.DeploymentStatusPending, TriggerType: models.TriggerTypeManual}
	pending := &models.Deployment{EnvironmentID: env.ID, Status: models.DeploymentStatusPending, TriggerType: models.TriggerTypeManual}
	for _, d := range []*models.Deployment{running, pending} {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create(deployment) error = %v", err)
		}
	}
	if err := repo.UpdateStatus(ctx, running.ID, models.DeploymentStatusRunning, nil); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	got, err := repo.ListByStatus(ctx, models.DeploymentStatusRunning)
	if err != nil {
		t.Fatalf("ListByStatus() error = %v", err)
	}
	var found bool
	for _, d := range got {
		if d.Status != models.DeploymentStatusRunning {
			t.Fatalf("ListByStatus(running) returned %s deployment %s", d.Status, d.ID)
		}
		if d.ID == pending.ID {
			t.Fatalf("ListByStatus(running) returned pending deployment %s", d.ID)
		}
		if d.ID == running.ID {
			found = true
			if !d.FinishedAt.Valid {
				t.Fatal("running deployment has no finished_at; recovery measures grace from it")
			}
		}
	}
	if !found {
		t.Fatalf("ListByStatus(running) missing deployment %s", running.ID)
	}
}
