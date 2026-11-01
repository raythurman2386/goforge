package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
)

// Project delete needs the typed project name. Uses DATABASE_URL (skips
// when no database is reachable), like the repository tests.
func TestProjectHandler_Delete_RequiresTypedName(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("skipping; no database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.MigrateUp(db, cfg); err != nil {
		t.Skipf("skipping; migrate: %v", err)
	}

	ctx := context.Background()
	users := repositories.NewUserRepository(db.DB, nil)
	user := &models.User{Email: "typed-delete-" + uuid.NewString() + "@example.com"}
	if err = users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	projects := repositories.NewProjectRepository(db.DB)
	envs := repositories.NewEnvironmentRepository(db.DB)
	deps := repositories.NewDeploymentRepository(db.DB)
	ctrs := repositories.NewContainerRepository(db.DB)
	svc := project.NewService(projects, envs, repositories.NewEnvVariableRepository(db.DB, nil), deps, ctrs, nil, nil, nil)
	p, err := svc.CreateProject(ctx, &models.Project{
		UserID: user.ID, Name: "typed-delete-" + time.Now().Format("150405.000"),
		RepositoryURL: "https://example.com/r.git", DefaultBranch: "main", DockerfilePath: "Dockerfile", BuildContext: ".",
		HealthCheckPath: "/", HealthCheckInterval: 30,
	})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewProjectHandler(svc, nil, deps, ctrs, nil, nil, logger)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	del := func(confirm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/projects/"+p.ID+"?"+url.Values{"confirm_name": {confirm}}.Encode(), nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", p.ID)
		c := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		c = context.WithValue(c, auth.UserContextKey, user)
		rr := httptest.NewRecorder()
		h.DeleteProject(rr, req.WithContext(c))
		return rr
	}

	if rr := del("not-the-name"); rr.Code != http.StatusBadRequest {
		t.Fatalf("wrong name: status=%d", rr.Code)
	}
	if _, err := svc.GetProject(ctx, p.ID); err != nil {
		t.Fatalf("project gone after a refused delete: %v", err)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("refused delete wrote %d audit rows", n)
	}

	rr := del(p.Name)
	if rr.Code != http.StatusOK || rr.Header().Get("HX-Redirect") != "/projects" {
		t.Fatalf("right name: status=%d redirect=%q", rr.Code, rr.Header().Get("HX-Redirect"))
	}
	if _, err := svc.GetProject(ctx, p.ID); err == nil {
		t.Fatal("project still there")
	}
	if rows := rec.snapshot(); len(rows) != 1 || rows[0].action != models.AuditActionProjectDelete {
		t.Fatalf("rows = %+v", rows)
	}
}
