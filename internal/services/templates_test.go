package services

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

const repoTemplatesDir = "../../templates"

func loadRepoCatalog(t *testing.T) *Catalog {
	t.Helper()
	c := NewCatalog(repoTemplatesDir)
	if err := c.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates(%s) error = %v", repoTemplatesDir, err)
	}
	return c
}

func TestRepoTemplates_WebServices(t *testing.T) {
	t.Parallel()
	c := loadRepoCatalog(t)

	tests := []struct {
		name     string
		image    string
		port     int
		category string
	}{
		{name: "solidcraft", image: "ghcr.io/tangency-labs/solidcraft-web:{{ .Version }}", port: 8080, category: "cad"},
		{name: "vaultwarden", image: "vaultwarden/server:{{ .Version }}", port: 80, category: "security"},
		{name: "gitea", image: "gitea/gitea:{{ .Version }}", port: 3000, category: "devtools"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpl, err := c.GetTemplate(tt.name)
			if err != nil {
				t.Fatalf("GetTemplate(%q) error = %v", tt.name, err)
			}
			if !tmpl.Web {
				t.Errorf("%s: web = false, want true", tt.name)
			}
			if tmpl.Category != tt.category {
				t.Errorf("%s: category = %q, want %q", tt.name, tmpl.Category, tt.category)
			}
			if tmpl.Docker.Image != tt.image {
				t.Errorf("%s: image = %q, want %q", tt.name, tmpl.Docker.Image, tt.image)
			}
			if len(tmpl.Docker.Ports) == 0 || tmpl.Docker.Ports[0].Container != tt.port {
				t.Errorf("%s: first port = %v, want %d", tt.name, tmpl.Docker.Ports, tt.port)
			}
			if len(tmpl.Docker.HealthCheck.Test) == 0 {
				t.Errorf("%s: missing health check", tt.name)
			}
			if !containsString(tmpl.Versions, tmpl.DefaultVersion) {
				t.Errorf("%s: default_version %q not in versions %v", tt.name, tmpl.DefaultVersion, tmpl.Versions)
			}
		})
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestPublicProtocol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		domain string
		want   string
	}{
		{domain: "", want: "http"},
		{domain: "localhost", want: "http"},
		{domain: "vaultwarden.localhost", want: "http"},
		{domain: "vaultwarden-app.example.com", want: "https"},
	}
	for _, tt := range tests {
		if got := publicProtocol(tt.domain); got != tt.want {
			t.Errorf("publicProtocol(%q) = %q, want %q", tt.domain, got, tt.want)
		}
	}
}

// deployRepoTemplate deploys a repo template through InstanceService with mock
// Docker and returns the container options GoForge would create.
func deployRepoTemplate(t *testing.T, tmplName string, proxyMode bool, config map[string]interface{}) docker.ContainerOptions {
	t.Helper()
	c := loadRepoCatalog(t)

	optsCh := make(chan docker.ContainerOptions, 1)
	done := make(chan struct{})
	var once sync.Once
	repo := &mockInstanceRepository{
		createFunc: func(_ context.Context, s *models.ServiceInstance) error {
			s.ID = "inst-" + tmplName
			return nil
		},
		getByIDFunc: func(_ context.Context, id string) (*models.ServiceInstance, error) {
			return &models.ServiceInstance{ID: id}, nil
		},
		updateFunc: func(_ context.Context, _ *models.ServiceInstance) error {
			once.Do(func() { close(done) })
			return nil
		},
	}
	dc := &mockDockerClient{
		createContainerFunc: func(_ context.Context, opts docker.ContainerOptions) (string, error) {
			optsCh <- opts
			return "docker-" + tmplName, nil
		},
	}
	svc := NewInstanceService(repo, &mockContainerRepository{}, dc, c, "traefik", proxyMode, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := svc.DeployInstance(context.Background(), "user1", tmplName, config); err != nil {
		t.Fatalf("DeployInstance(%s) error = %v", tmplName, err)
	}

	var opts docker.ContainerOptions
	select {
	case opts = <-optsCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for CreateContainer")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for status update")
	}
	return opts
}

func envMap(opts docker.ContainerOptions) map[string]string {
	env := map[string]string{}
	for _, kv := range opts.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return env
}

// assertWebRoute checks for a Traefik Host rule on domain and, in proxy mode,
// that every router uses the web entrypoint.
func assertWebRoute(t *testing.T, opts docker.ContainerOptions, domain string, wantPort string) {
	t.Helper()
	foundRule := false
	for k, v := range opts.Labels {
		if strings.HasSuffix(k, ".rule") && strings.Contains(v, domain) {
			foundRule = true
		}
		if strings.HasSuffix(k, ".entrypoints") && v != "web" {
			t.Errorf("entrypoint label %s = %q, want web in proxy mode", k, v)
		}
		if wantPort != "" && strings.HasSuffix(k, ".loadbalancer.server.port") && v != wantPort {
			t.Errorf("service port label %s = %q, want %s", k, v, wantPort)
		}
	}
	if !foundRule {
		t.Errorf("no Traefik Host rule for %s in labels %v", domain, opts.Labels)
	}
}

// Cloudflare proxy mode routes HTTP to Traefik, but visitors still use HTTPS.
// Vaultwarden must be told its public https:// URL or the web vault breaks.
func TestDeployVaultwarden_CloudflareProxyModeUsesHTTPSDomain(t *testing.T) {
	t.Parallel()
	domain := "vaultwarden-app.example.com"
	opts := deployRepoTemplate(t, "vaultwarden", true, map[string]interface{}{
		"name":            "vault",
		"domain":          domain,
		"signups_allowed": "false",
	})

	env := envMap(opts)
	if got, want := env["DOMAIN"], "https://"+domain; got != want {
		t.Errorf("DOMAIN = %q, want %q", got, want)
	}
	if got := env["SIGNUPS_ALLOWED"]; got != "false" {
		t.Errorf("SIGNUPS_ALLOWED = %q, want false", got)
	}
	if got := len(env["ADMIN_TOKEN"]); got != 48 {
		t.Errorf("generated ADMIN_TOKEN length = %d, want 48", got)
	}
	if opts.Image != "vaultwarden/server:latest" {
		t.Errorf("image = %q, want vaultwarden/server:latest", opts.Image)
	}
	assertWebRoute(t, opts, domain, "")
}

// Gitea behind Cloudflare Tunnel + Traefik must keep listening on plain HTTP
// (PROTOCOL=http). Only ROOT_URL carries the https:// browser URL. Setting
// PROTOCOL=https makes Gitea expect TLS on :3000 and the route breaks.
func TestDeployGitea_CloudflareProxyModeKeepsHTTPListener(t *testing.T) {
	t.Parallel()
	domain := "gitea-app.example.com"
	opts := deployRepoTemplate(t, "gitea", true, map[string]interface{}{
		"name":                 "git",
		"domain":               domain,
		"app_name":             "Example Git",
		"disable_registration": "true",
		"require_signin_view":  "true",
	})

	env := envMap(opts)
	want := map[string]string{
		"GITEA__SERVER__PROTOCOL":              "http",
		"GITEA__SERVER__ROOT_URL":              "https://" + domain + "/",
		"GITEA__SERVER__DOMAIN":                domain,
		"GITEA__SERVER__HTTP_PORT":             "3000",
		"GITEA__SERVER__SSH_PORT":              "22",
		"GITEA____APP_NAME":                    "Example Git",
		"GITEA__SERVICE__DISABLE_REGISTRATION": "true",
		"GITEA__SERVICE__REQUIRE_SIGNIN_VIEW":  "true",
	}
	for k, v := range want {
		if got := env[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// GITEA__APP_NAME has no section separator, so Gitea ignores it.
	if _, ok := env["GITEA__APP_NAME"]; ok {
		t.Error("GITEA__APP_NAME is set; Gitea ignores it, use GITEA____APP_NAME")
	}
	if opts.Image != "gitea/gitea:1.25.5" {
		t.Errorf("image = %q, want gitea/gitea:1.25.5", opts.Image)
	}
	if len(opts.Binds) != 1 || !strings.HasSuffix(opts.Binds[0], ":/data") {
		t.Errorf("binds = %v, want one volume on /data", opts.Binds)
	}
	assertWebRoute(t, opts, domain, "3000")
}

// Without a domain Gitea has no public URL, so ROOT_URL and DOMAIN point at the
// container on the Docker network.
func TestDeployGitea_NoDomainUsesContainerURL(t *testing.T) {
	t.Parallel()
	opts := deployRepoTemplate(t, "gitea", false, map[string]interface{}{
		"name":                 "git",
		"app_name":             "Example Git",
		"disable_registration": "false",
		"require_signin_view":  "false",
	})

	env := envMap(opts)
	if got, want := env["GITEA__SERVER__ROOT_URL"], "http://"+opts.Name+":3000/"; got != want {
		t.Errorf("ROOT_URL = %q, want %q", got, want)
	}
	if got := env["GITEA__SERVER__DOMAIN"]; got != opts.Name {
		t.Errorf("DOMAIN = %q, want %q", got, opts.Name)
	}
	if got := env["GITEA__SERVER__PROTOCOL"]; got != "http" {
		t.Errorf("PROTOCOL = %q, want http", got)
	}
	if got := env["GITEA__SERVICE__DISABLE_REGISTRATION"]; got != "false" {
		t.Errorf("DISABLE_REGISTRATION = %q, want false", got)
	}
}
