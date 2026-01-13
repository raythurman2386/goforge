package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/user/gocool/internal/config"
	"github.com/user/gocool/internal/services"
)

func setupTestRouterDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	return db
}

func TestNewRouter(t *testing.T) {
	cfg := &config.Config{
		DevMode:  true,
		Port:     8080,
		LogLevel: "info",
	}
	db := setupTestRouterDB(t)
	defer db.Close()
	catalog := services.NewCatalog("./templates")

	router := NewRouter(cfg, db, catalog)
	if router == nil {
		t.Fatal("NewRouter() returned nil")
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
	catalog := services.NewCatalog("./templates")

	router := NewRouter(cfg, db, catalog)
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
	catalog := services.NewCatalog("./templates")

	router := NewRouter(cfg, db, catalog)
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
	catalog := services.NewCatalog("./templates")

	router := NewRouter(cfg, db, catalog)
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
	catalog := services.NewCatalog("./templates")

	router := NewRouter(cfg, db, catalog)
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
	catalog := services.NewCatalog("./templates")

	router := NewRouter(cfg, db, catalog)
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
