package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/handlers"
	"github.com/raythurman2386/goforge/internal/webhook"
)

func setupTestRouterDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	return db
}

func setupTestRouter(t *testing.T, db *sql.DB, cfg *config.Config) http.Handler {
	t.Helper()

	// Repos
	userRepo := repositories.NewUserRepository(db)
	sessionRepo := repositories.NewSessionRepository(db)
	projectRepo := repositories.NewProjectRepository(db)
	envRepo := repositories.NewEnvironmentRepository(db)
	varRepo := repositories.NewEnvVariableRepository(db)
	instanceRepo := repositories.NewServiceInstanceRepository(db)

	// Services
	sm := auth.NewSessionManager(sessionRepo, userRepo)
	ps := project.NewService(projectRepo, envRepo, varRepo)
	// We can pass nil for deployService dependencies that aren't used in these tests
	ds := &deploy.Service{}
	is := services.NewInstanceService(instanceRepo, nil, nil, nil, "goforge_net")
	cat := services.NewCatalog("./templates")

	// Monitor
	mon := monitor.NewCollector(nil, nil, projectRepo, instanceRepo)

	// Handlers
	ah := handlers.NewAuthHandler(sm, userRepo, nil, nil)
	dbh := handlers.NewDashboardHandler(mon)
	ph := handlers.NewProjectHandler(ps, nil, nil)
	eh := handlers.NewEnvironmentHandler(ps)
	dh := handlers.NewDeploymentHandler(ds, ps, nil, nil)
	sh := handlers.NewServiceHandler(cat, is)
	sth := handlers.NewSettingsHandler(nil) // No GitSourceRepo needed for general router tests
	gh := handlers.NewGitHandler(nil, nil)
	wh := &webhook.Handler{}
	sseh := handlers.NewSSEHandler(nil, nil, nil)

	return Router(cfg, sm, ah, dbh, ph, eh, dh, sh, sth, gh, wh, sseh)
}
func TestRouter(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	if router == nil {
		t.Fatal("Router() returned nil")
	}
}

func TestRouter_HealthEndpoint(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestRouter_HealthReadyEndpoint(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health/ready")
	if err != nil {
		t.Fatalf("GET /health/ready failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health/ready status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestRouter_IndexRedirect(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	server := httptest.NewServer(router)
	defer server.Close()

	// Disable redirects to capture the redirect response
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("GET / status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	location := resp.Header.Get("Location")
	if location != "/login" {
		t.Errorf("GET / Location = %q, want %q", location, "/login")
	}
}

func TestRouter_LoginPage(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestRouter_NotFound(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	server := httptest.NewServer(router)
	defer server.Close()

	resp, err := http.Get(server.URL + "/nonexistent-route")
	if err != nil {
		t.Fatalf("GET /nonexistent-route failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /nonexistent-route status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
