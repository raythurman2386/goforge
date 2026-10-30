package web

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/handlers"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/middleware"
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
	return setupTestRouterWithAPI(t, db, cfg, nil)
}

func setupTestRouterWithAPI(t *testing.T, db *sql.DB, cfg *config.Config, apiV1 http.Handler) http.Handler {
	t.Helper()
	return setupTestRouterWithMCP(t, db, cfg, apiV1, nil)
}

func setupTestRouterWithMCP(t *testing.T, db *sql.DB, cfg *config.Config, apiV1, mcpHTTP http.Handler) http.Handler {
	t.Helper()

	// Repos
	userRepo := repositories.NewUserRepository(db, nil)
	sessionRepo := repositories.NewSessionRepository(db)
	projectRepo := repositories.NewProjectRepository(db)
	envRepo := repositories.NewEnvironmentRepository(db)
	varRepo := repositories.NewEnvVariableRepository(db, nil)
	instanceRepo := repositories.NewServiceInstanceRepository(db)

	// Services
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sm := auth.NewSessionManager(sessionRepo, userRepo, logger)
	ps := project.NewService(projectRepo, envRepo, varRepo, nil, nil, nil, nil, nil)
	// We can pass nil for deployService dependencies that aren't used in these tests
	ds := &deploy.Service{}
	is := services.NewInstanceService(instanceRepo, nil, nil, nil, "goforge_net", false, logger)
	cat := services.NewCatalog("./templates")

	// Audit Service (mocking repo implicitly by passing nil? No, NewAuditService takes interface, but we need concrete impl or mock.
	// For now, let's create a repo with the test DB since we have it.)
	auditRepo := repositories.NewAuditLogRepository(db)
	as := services.NewAuditService(auditRepo, logger)

	// Monitor
	mon := monitor.NewCollector(nil, nil, projectRepo, instanceRepo)

	// Handlers
	ah := handlers.NewAuthHandler(sm, userRepo, nil, nil, as, cfg.EnableRegistration, logger)
	dbh := handlers.NewDashboardHandler(mon, is, nil, nil, logger)
	ph := handlers.NewProjectHandler(ps, nil, nil, nil, nil, is, logger)
	eh := handlers.NewEnvironmentHandler(ps, logger)
	dh := handlers.NewDeploymentHandler(ds, ps, nil, nil, logger)
	sh := handlers.NewServiceHandler(cat, is, logger)
	sth := handlers.NewSettingsHandler(nil, is, userRepo, nil, logger) // Settings handler
	gh := handlers.NewGitHandler(nil, nil, logger)
	wh := webhook.NewHandler(nil, nil, nil, nil, "", logger)
	sseh := handlers.NewSSEHandler(nil, nil, nil, nil, nil, nil, nil, logger)

	hh := handlers.NewHealthHandler(nil, nil, logger)
	ch := handlers.NewContainerHandler(nil, nil, nil, logger) // Container handler with nil dependencies for testing
	rl := middleware.NewRateLimiter(100, 200, httputil.ProxyTrust{})
	t.Cleanup(rl.Stop)
	return Router(cfg, sm, nil, nil, rl, ah, dbh, ph, eh, dh, sh, sth, gh, wh, sseh, hh, ch, apiV1, mcpHTTP)
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

func TestRouter_DeploymentAndContainerRoutesExist(t *testing.T) {
	cfg := &config.Config{DevMode: true, Port: 8080, LogLevel: "info"}
	db := setupTestRouterDB(t)
	defer db.Close()

	router := setupTestRouter(t, db, cfg)
	server := httptest.NewServer(router)
	defer server.Close()

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	paths := []string{
		"/projects/p1/deployments/d1",
		"/projects/p1/deployments/d1/logs",
		"/containers/abc",
		"/containers/abc/logs",
		"/containers/abc/stats",
	}
	for _, path := range paths {
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("GET %s = 404", path)
		}
	}

	postPaths := []string{
		"/projects/p1/deployments/d1/cancel",
		"/containers/abc/stop",
		"/containers/abc/restart",
	}
	for _, path := range postPaths {
		resp, err := client.Post(server.URL+path, "application/x-www-form-urlencoded", nil)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("POST %s = 404", path)
		}
	}
}

// TestRouter_APIv1OutsideCSRFAndSession checks that /api/v1 is mounted
// outside the CSRF group and without the session middleware, while the HTML
// routes keep CSRF.
func TestRouter_APIv1OutsideCSRFAndSession(t *testing.T) {
	cfg := &config.Config{DevMode: true, Port: 8080, LogLevel: "info"}
	db := setupTestRouterDB(t)
	defer db.Close()

	var reached bool
	var sawUser bool
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		sawUser = r.Context().Value(auth.UserContextKey) != nil
		w.WriteHeader(http.StatusTeapot)
	})
	router := setupTestRouterWithAPI(t, db, cfg, http.StripPrefix("/api/v1", stub))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/services/x/restart", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	req.AddCookie(&http.Cookie{Name: "session", Value: "anything"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if !reached || rec.Code != http.StatusTeapot {
		t.Fatalf("POST /api/v1 without CSRF token: reached=%v status=%d, want the API handler", reached, rec.Code)
	}
	if sawUser {
		t.Error("session middleware ran on /api/v1")
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /auth/logout without CSRF token = %d, want 403", rec.Code)
	}
}

// TestRouter_MCPOutsideCSRFAndSession checks that /mcp is mounted beside
// /api/v1 (no CSRF, no session middleware) and is absent when the kill
// switch leaves the handler nil.
func TestRouter_MCPOutsideCSRFAndSession(t *testing.T) {
	cfg := &config.Config{DevMode: true, Port: 8080, LogLevel: "info"}
	db := setupTestRouterDB(t)
	defer db.Close()

	var reached, sawUser bool
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		sawUser = r.Context().Value(auth.UserContextKey) != nil
		w.WriteHeader(http.StatusTeapot)
	})
	router := setupTestRouterWithMCP(t, db, cfg, nil, stub)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer some-token")
	req.AddCookie(&http.Cookie{Name: "session", Value: "anything"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if !reached || rec.Code != http.StatusTeapot {
		t.Fatalf("POST /mcp without CSRF token: reached=%v status=%d, want the MCP handler", reached, rec.Code)
	}
	if sawUser {
		t.Error("session middleware ran on /mcp")
	}

	off := setupTestRouterWithMCP(t, db, cfg, nil, nil)
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code == http.StatusTeapot || rec.Code < 400 {
		t.Fatalf("POST /mcp with GOFORGE_MCP_HTTP=false = %d, want an error", rec.Code)
	}
}

// TestRouter_ContainersAPINotShadowedByDetail checks that GET /containers/api
// reaches the list API route, not /containers/{id} with id="api".
func TestRouter_ContainersAPINotShadowedByDetail(t *testing.T) {
	cfg := &config.Config{DevMode: true, Port: 8080, LogLevel: "info"}
	db := setupTestRouterDB(t)
	defer db.Close()

	routes, ok := setupTestRouter(t, db, cfg).(chi.Routes)
	if !ok {
		t.Fatal("Router() does not implement chi.Routes")
	}

	cases := map[string]string{
		"/containers":     "/containers",
		"/containers/api": "/containers/api",
		"/containers/abc": "/containers/{id}",
	}
	for path, want := range cases {
		got := routes.Find(chi.NewRouteContext(), http.MethodGet, path)
		if got != want {
			t.Errorf("GET %s matched %q, want %q", path, got, want)
		}
	}
}

// Browsers fetch /favicon.ico on every page, so it must not 404. Static files
// are served relative to the working directory, as in the container.
func TestRouter_FaviconAndFont(t *testing.T) {
	t.Chdir("../..")
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()

	server := httptest.NewServer(setupTestRouter(t, db, cfg))
	defer server.Close()

	for _, path := range []string{"/favicon.ico", "/static/favicon.svg", "/static/fonts/InterVariable.woff2"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) == 0 {
			t.Errorf("GET %s = %d (%d bytes), want 200 with a body", path, resp.StatusCode, len(body))
		}
	}
}

func TestRouter_DevUIGalleryOnlyInDevMode(t *testing.T) {
	for _, tt := range []struct {
		devMode bool
		want    int
	}{
		{true, http.StatusOK},
		{false, http.StatusNotFound},
	} {
		cfg := &config.Config{DevMode: tt.devMode, Port: 8080, LogLevel: "info", CSRFSecret: "test-csrf-secret"}
		db := setupTestRouterDB(t)
		router := setupTestRouter(t, db, cfg)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/ui", nil))
		db.Close()
		if rec.Code != tt.want {
			t.Errorf("DevMode=%v: GET /dev/ui = %d, want %d", tt.devMode, rec.Code, tt.want)
		}
		if tt.devMode && !strings.Contains(rec.Body.String(), "UI kit gallery") {
			t.Errorf("DevMode=true: gallery heading missing")
		}
	}
}

func TestRouter_DevUIDialogDemoOnlyInDevMode(t *testing.T) {
	for _, tt := range []struct {
		devMode bool
		query   string
		want    int
		body    string
	}{
		{true, "?depth=1", http.StatusOK, `data-ui-dialog="ephemeral"`},
		{true, "?ok=1", http.StatusNoContent, ""},
		{true, "?fail=1", http.StatusUnprocessableEntity, "Demo error"},
		{false, "?depth=1", http.StatusNotFound, ""},
	} {
		cfg := &config.Config{DevMode: tt.devMode, Port: 8080, LogLevel: "info", CSRFSecret: "test-csrf-secret"}
		db := setupTestRouterDB(t)
		router := setupTestRouter(t, db, cfg)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/ui/dialog"+tt.query, nil))
		db.Close()
		if rec.Code != tt.want {
			t.Errorf("DevMode=%v %s: code %d, want %d", tt.devMode, tt.query, rec.Code, tt.want)
		}
		if tt.body != "" && !strings.Contains(rec.Body.String(), tt.body) {
			t.Errorf("DevMode=%v %s: body missing %q", tt.devMode, tt.query, tt.body)
		}
	}
}
