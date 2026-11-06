package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
)

// This drive uses the real deployment/project services and rollback manager;
// fake rows would prove serialization but not the terminal-state wiring.
func TestDeploymentStatusUpdatesSwapManagerEligibleActions(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("integration database unavailable: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx := context.Background()
	user := &models.User{Email: "sse-actions-" + uuid.NewString() + "@example.test"}
	if err := repositories.NewUserRepository(db.DB, nil).Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	defer func() {
		deleteCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(deleteCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Error(err)
		}
	}()
	proj := &models.Project{UserID: user.ID, Name: "sse-actions"}
	if err := repositories.NewProjectRepository(db.DB).Create(ctx, proj); err != nil {
		t.Fatal(err)
	}
	env := &models.Environment{ProjectID: proj.ID, Name: "verify"}
	if err := repositories.NewEnvironmentRepository(db.DB).Create(ctx, env); err != nil {
		t.Fatal(err)
	}
	d := &models.Deployment{EnvironmentID: env.ID, Status: models.DeploymentStatusBuilding, TriggerType: models.TriggerTypeManual}
	deploymentRepo := repositories.NewDeploymentRepository(db.DB)
	if err := deploymentRepo.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	deploySvc := deploy.NewService(deploymentRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "traefik", false, t.TempDir(), logger)
	projectSvc := project.NewService(nil, repositories.NewEnvironmentRepository(db.DB), nil, nil, nil, nil, nil, nil)
	h := &SSEHandler{deployService: deploySvc, projectService: projectSvc, rollbackManager: deploy.NewRollbackManager(deploySvc), logger: logger}
	r := httptest.NewRequest("GET", "/projects/p/deployments/d/logs", nil)

	w := httptest.NewRecorder()
	h.writeDeploymentStatusUpdate(w, w, r, d.ID, models.DeploymentStatusBuilding)
	if active := w.Body.String(); !strings.Contains(active, "Cancel<svg") || strings.Contains(active, "Rollback<svg") {
		t.Fatalf("active actions = %q", active)
	}
	d.Status = models.DeploymentStatusRunning
	if err := deploymentRepo.UpdateStatus(ctx, d.ID, d.Status, nil); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.writeDeploymentStatusUpdate(w, w, r, d.ID, models.DeploymentStatusRunning)
	if terminal := w.Body.String(); !strings.Contains(terminal, "Rollback<svg") || strings.Contains(terminal, "Cancel<svg") {
		t.Fatalf("terminal actions = %q", terminal)
	}
}
