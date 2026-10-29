package proxy

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

const testRouterName = "goforge-proj-123-production"

func TestGenerateLabels_BasicLabels(t *testing.T) {
	p := &models.Project{
		ID:   "proj-123",
		Name: "test-project",
	}
	env := &models.Environment{
		ID:   "env-456",
		Name: "production",
	}

	labels := GenerateLabels(p, env, "traefik")

	// Check basic labels
	assertLabel(t, labels, "traefik.enable", "true")
	assertLabel(t, labels, "traefik.docker.network", "traefik")
	assertLabel(t, labels, "goforge.managed", "true")
	assertLabel(t, labels, "goforge.project.id", "proj-123")
	assertLabel(t, labels, "goforge.environment.id", "env-456")
}

func TestGenerateLabels_WithDomain(t *testing.T) {
	p := &models.Project{
		ID:   "proj-123",
		Name: "test-project",
	}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
		Port:   sql.NullInt32{Int32: 3000, Valid: true},
	}

	labels := GenerateLabels(p, env, "traefik")

	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".rule", "Host(`example.com`)")
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".entrypoints", "websecure")
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".tls.certresolver", "letsencrypt")
	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.server.port", "3000")
}

func TestGenerateLabels_WithDefaultPort(t *testing.T) {
	p := &models.Project{
		ID:   "proj-123",
		Name: "test-project",
	}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
		// No port set - should default to 80
	}

	labels := GenerateLabels(p, env, "traefik")

	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.server.port", "80")
}

func TestGenerateLabelsWithOptions_HTTPRedirect(t *testing.T) {
	p := &models.Project{
		ID:   "proj-123",
		Name: "test-project",
	}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		EnableHTTPRedirect: true,
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	// Should have HTTP router for redirect
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+"-http.rule", "Host(`example.com`)")
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+"-http.entrypoints", "web")
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+"-http.middlewares", testRouterName+"-redirect")

	// Should have redirect middleware
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-redirect.redirectscheme.scheme", "https")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-redirect.redirectscheme.permanent", "true")
}

func TestGenerateLabelsWithOptions_RateLimit(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		RateLimit: &RateLimitConfig{
			Average: 100,
			Burst:   50,
		},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-ratelimit.ratelimit.average", "100")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-ratelimit.ratelimit.burst", "50")
	assertContains(t, labels, "traefik.http.routers."+testRouterName+".middlewares", testRouterName+"-ratelimit")
}

func TestGenerateLabelsWithOptions_Headers(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		Headers: &HeadersConfig{
			STSSeconds:           31536000,
			STSIncludeSubdomains: true,
			STSPreload:           true,
			ForceSTSHeader:       true,
			ContentTypeNosniff:   true,
			BrowserXSSFilter:     true,
			FrameDeny:            true,
		},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.stsSeconds", "31536000")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.stsIncludeSubdomains", "true")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.stsPreload", "true")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.forceSTSHeader", "true")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.contentTypeNosniff", "true")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.browserXssFilter", "true")
	assertLabel(t, labels, "traefik.http.middlewares."+testRouterName+"-headers.headers.frameDeny", "true")
}

func TestGenerateLabelsWithOptions_PathPrefix(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "api",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		PathPrefix: "/api/v1",
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	name := "goforge-proj-123-api"
	assertLabel(t, labels, "traefik.http.routers."+name+".rule", "Host(`example.com`) && PathPrefix(`/api/v1`)")
}

func TestGenerateLabelsWithOptions_StripPrefix(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "api",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		PathPrefix:  "/api/v1",
		StripPrefix: true,
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	name := "goforge-proj-123-api"
	assertLabel(t, labels, "traefik.http.middlewares."+name+"-stripprefix.stripprefix.prefixes", "/api/v1")
	assertContains(t, labels, "traefik.http.routers."+name+".middlewares", name+"-stripprefix")
}

func TestGenerateLabelsWithOptions_HealthCheck(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		HealthCheck: &HealthCheckConfig{
			Path:     "/health",
			Interval: "10s",
			Timeout:  "3s",
		},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.healthcheck.path", "/health")
	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.healthcheck.interval", "10s")
	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.healthcheck.timeout", "3s")
}

func TestGenerateLabelsWithOptions_BasicAuth(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "staging",
		Domain: sql.NullString{String: "staging.example.com", Valid: true},
	}

	opts := LabelOptions{
		BasicAuth: &BasicAuthConfig{
			Users: []string{"admin:$apr1$xxx$yyy"},
		},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	name := "goforge-proj-123-staging"
	assertLabel(t, labels, "traefik.http.middlewares."+name+"-basicauth.basicauth.users", "admin:$apr1$xxx$yyy")
	assertContains(t, labels, "traefik.http.routers."+name+".middlewares", name+"-basicauth")
}

func TestGenerateLabelsWithOptions_IPWhitelist(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "internal",
		Domain: sql.NullString{String: "internal.example.com", Valid: true},
	}

	opts := LabelOptions{
		IPWhitelist: []string{"10.0.0.0/8", "192.168.1.0/24"},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	name := "goforge-proj-123-internal"
	assertLabel(t, labels, "traefik.http.middlewares."+name+"-ipwhitelist.ipwhitelist.sourcerange", "10.0.0.0/8,192.168.1.0/24")
	assertContains(t, labels, "traefik.http.routers."+name+".middlewares", name+"-ipwhitelist")
}

func TestGenerateLabelsWithOptions_CustomMiddlewares(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		CustomMiddlewares: []string{"my-auth@docker", "my-compress@file"},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	assertContains(t, labels, "traefik.http.routers."+testRouterName+".middlewares", "my-auth@docker")
	assertContains(t, labels, "traefik.http.routers."+testRouterName+".middlewares", "my-compress@file")
}

func TestGenerateLabelsWithOptions_Priority(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		Priority: 100,
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".priority", "100")
}

func TestGenerateLabelsWithOptions_CombinedMiddlewares(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		EnableHTTPRedirect: true,
		RateLimit: &RateLimitConfig{
			Average: 100,
			Burst:   50,
		},
		Headers: &HeadersConfig{
			ContentTypeNosniff: true,
		},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	middlewares := labels["traefik.http.routers."+testRouterName+".middlewares"]

	// Should contain both middlewares
	if middlewares == "" {
		t.Error("expected middlewares label to be set")
	}
}

func TestGenerateLabelsWithOptions_NoDomain(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:   "env-456",
		Name: "production",
		// No domain set
	}

	opts := LabelOptions{
		EnableHTTPRedirect: true,
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	// Should still have basic labels
	assertLabel(t, labels, "traefik.enable", "true")
	assertLabel(t, labels, "goforge.managed", "true")

	// But no routing labels
	if _, ok := labels["traefik.http.routers."+testRouterName+".rule"]; ok {
		t.Error("expected no router rule without domain")
	}
}

func TestGenerateLabelsWithOptions_Sticky(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:       "env-456",
		Name:     "production",
		Domain:   sql.NullString{String: "example.com", Valid: true},
		Replicas: 3,
	}

	opts := LabelOptions{
		Sticky: &StickyConfig{
			CookieName: "goforge_session",
			Secure:     true,
			HTTPOnly:   true,
		},
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.sticky.cookie.name", "goforge_session")
	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.sticky.cookie.secure", "true")
	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.sticky.cookie.httpOnly", "true")
}

func TestGenerateLabels_WithProjectDomain(t *testing.T) {
	p := &models.Project{
		ID:           "proj-123",
		Name:         "test-project",
		CustomDomain: sql.NullString{String: "project.com", Valid: true},
	}
	env := &models.Environment{
		ID:   "env-456",
		Name: "production",
		// No env domain set
	}

	labels := GenerateLabels(p, env, "traefik")

	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".rule", "Host(`project.com`)")
}

func TestGenerateLabels_WithProjectDomainOverride(t *testing.T) {
	p := &models.Project{
		ID:           "proj-123",
		Name:         "test-project",
		CustomDomain: sql.NullString{String: "project.com", Valid: true},
	}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "env.com", Valid: true},
	}

	labels := GenerateLabels(p, env, "traefik")

	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".rule", "Host(`env.com`)")
}

func TestGenerateLabelsWithOptions_CloudflareProxyMode(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
		Port:   sql.NullInt32{Int32: 3000, Valid: true},
	}

	opts := LabelOptions{
		CloudflareProxyMode: true,
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	// Should use HTTP entrypoint (Cloudflare handles SSL)
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".rule", "Host(`example.com`)")
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".entrypoints", "web")
	assertLabel(t, labels, "traefik.http.services."+testRouterName+".loadbalancer.server.port", "3000")

	// Should NOT have TLS/certresolver labels
	if _, ok := labels["traefik.http.routers."+testRouterName+".tls.certresolver"]; ok {
		t.Error("expected no TLS certresolver in Cloudflare mode")
	}
}

func TestGenerateLabelsWithOptions_CloudflareProxyMode_SkipsHTTPRedirect(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	opts := LabelOptions{
		CloudflareProxyMode: true,
		EnableHTTPRedirect:  true, // Should be ignored in Cloudflare mode
	}

	labels := GenerateLabelsWithOptions(p, env, "traefik", opts)

	// Should NOT have HTTP redirect router (Cloudflare enforces HTTPS)
	if _, ok := labels["traefik.http.routers."+testRouterName+"-http.rule"]; ok {
		t.Error("expected no HTTP redirect router in Cloudflare mode")
	}
	if _, ok := labels["traefik.http.middlewares."+testRouterName+"-redirect.redirectscheme.scheme"]; ok {
		t.Error("expected no redirect middleware in Cloudflare mode")
	}
}

func TestGenerateLabelsForCloudflare(t *testing.T) {
	p := &models.Project{ID: "proj-123"}
	env := &models.Environment{
		ID:     "env-456",
		Name:   "production",
		Domain: sql.NullString{String: "example.com", Valid: true},
	}

	// Test with cloudflare mode enabled
	labels := GenerateLabelsForCloudflare(p, env, "traefik", true)
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".entrypoints", "web")

	// Test with cloudflare mode disabled (standard mode)
	labels = GenerateLabelsForCloudflare(p, env, "traefik", false)
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".entrypoints", "websecure")
	assertLabel(t, labels, "traefik.http.routers."+testRouterName+".tls.certresolver", "letsencrypt")
}

func TestGenerateName(t *testing.T) {
	tests := []struct {
		projectID string
		envName   string
		want      string
	}{
		{"proj-123", "production", "goforge-proj-123-production"},
		{"abc", "dev", "goforge-abc-dev"},
		{"123-456", "staging-v2", "goforge-123-456-staging-v2"},
	}

	for _, tt := range tests {
		t.Run(tt.projectID+"-"+tt.envName, func(t *testing.T) {
			got := GenerateName(tt.projectID, tt.envName)
			if got != tt.want {
				t.Errorf("GenerateName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLabels_Merge(t *testing.T) {
	l1 := Labels{
		"key1": "value1",
		"key2": "value2",
	}
	l2 := Labels{
		"key2": "updated",
		"key3": "value3",
	}

	l1.Merge(l2)

	assertLabel(t, l1, "key1", "value1")
	assertLabel(t, l1, "key2", "updated")
	assertLabel(t, l1, "key3", "value3")
}

func TestLabels_ToMap(t *testing.T) {
	labels := Labels{
		"key1": "value1",
	}

	m := labels.ToMap()

	if m["key1"] != "value1" {
		t.Errorf("ToMap() did not preserve values")
	}

	// Modify the map and ensure it doesn't affect original
	m["key1"] = "modified"
	if labels["key1"] != "value1" {
		t.Errorf("ToMap() should return a copy")
	}
}

func TestGenerateServiceLabels_WithDomain(t *testing.T) {
	labels := GenerateServiceLabels("inst-abc", "gitea.example.com", "traefik", 3000, false)

	name := "goforge-svc-inst-abc"

	// Base labels
	assertLabel(t, labels, "traefik.enable", "true")
	assertLabel(t, labels, "traefik.docker.network", "traefik")
	assertLabel(t, labels, "goforge.managed", "true")
	assertLabel(t, labels, "goforge.service.instance.id", "inst-abc")

	// Routing labels (standard mode with TLS)
	assertLabel(t, labels, "traefik.http.routers."+name+".rule", "Host(`gitea.example.com`)")
	assertLabel(t, labels, "traefik.http.routers."+name+".entrypoints", "websecure")
	assertLabel(t, labels, "traefik.http.routers."+name+".tls.certresolver", "letsencrypt")
	assertLabel(t, labels, "traefik.http.services."+name+".loadbalancer.server.port", "3000")

	// HTTP-to-HTTPS redirect
	assertLabel(t, labels, "traefik.http.routers."+name+"-http.rule", "Host(`gitea.example.com`)")
	assertLabel(t, labels, "traefik.http.routers."+name+"-http.entrypoints", "web")
	assertLabel(t, labels, "traefik.http.routers."+name+"-http.middlewares", name+"-redirect")
	assertLabel(t, labels, "traefik.http.middlewares."+name+"-redirect.redirectscheme.scheme", "https")
	assertLabel(t, labels, "traefik.http.middlewares."+name+"-redirect.redirectscheme.permanent", "true")
}

func TestGenerateServiceLabels_NoDomain(t *testing.T) {
	for _, cloudflare := range []bool{false, true} {
		labels := GenerateServiceLabels("inst-abc", "", "traefik", 6379, cloudflare)
		// A data service with no domain must not get Traefik's default
		// Host(<container name>) router.
		assertLabel(t, labels, "traefik.enable", "false")
		for k := range labels {
			if strings.HasPrefix(k, "traefik.http.") {
				t.Errorf("cloudflare=%v: unexpected routing label %s without domain", cloudflare, k)
			}
		}
	}

	labels := GenerateServiceLabels("inst-abc", "", "traefik", 3000, false)

	name := "goforge-svc-inst-abc"

	// Should have base labels, with Traefik disabled
	assertLabel(t, labels, "traefik.enable", "false")
	assertLabel(t, labels, "traefik.docker.network", "traefik")
	assertLabel(t, labels, "goforge.managed", "true")
	assertLabel(t, labels, "goforge.service.instance.id", "inst-abc")

	// Should NOT have routing labels
	if _, ok := labels["traefik.http.routers."+name+".rule"]; ok {
		t.Error("expected no router rule without domain")
	}
	if _, ok := labels["traefik.http.services."+name+".loadbalancer.server.port"]; ok {
		t.Error("expected no port label without domain")
	}
}

func TestGenerateServiceLabels_CloudflareMode(t *testing.T) {
	labels := GenerateServiceLabels("inst-abc", "gitea.example.com", "traefik", 3000, true)

	name := "goforge-svc-inst-abc"

	assertLabel(t, labels, "traefik.enable", "true")

	// Should use HTTP entrypoint
	assertLabel(t, labels, "traefik.http.routers."+name+".rule", "Host(`gitea.example.com`)")
	assertLabel(t, labels, "traefik.http.routers."+name+".entrypoints", "web")
	assertLabel(t, labels, "traefik.http.services."+name+".loadbalancer.server.port", "3000")

	// Should NOT have TLS or redirect labels
	if _, ok := labels["traefik.http.routers."+name+".tls.certresolver"]; ok {
		t.Error("expected no TLS certresolver in Cloudflare mode")
	}
	if _, ok := labels["traefik.http.routers."+name+"-http.rule"]; ok {
		t.Error("expected no HTTP redirect router in Cloudflare mode")
	}
}

func TestGenerateServiceLabels_DefaultPort(t *testing.T) {
	labels := GenerateServiceLabels("inst-abc", "app.example.com", "traefik", 0, false)

	name := "goforge-svc-inst-abc"

	// Should default to port 80
	assertLabel(t, labels, "traefik.http.services."+name+".loadbalancer.server.port", "80")
}

func TestGenerateServiceLabels_CustomNetwork(t *testing.T) {
	labels := GenerateServiceLabels("inst-abc", "app.example.com", "custom-net", 8080, false)

	assertLabel(t, labels, "traefik.docker.network", "custom-net")
}

// Helper functions

func assertLabel(t *testing.T, labels Labels, key, want string) {
	t.Helper()
	got, ok := labels[key]
	if !ok {
		t.Errorf("expected label %q to exist", key)
		return
	}
	if got != want {
		t.Errorf("label %q = %q, want %q", key, got, want)
	}
}

func assertContains(t *testing.T, labels Labels, key, substr string) {
	t.Helper()
	got, ok := labels[key]
	if !ok {
		t.Errorf("expected label %q to exist", key)
		return
	}
	if got == "" {
		t.Errorf("label %q is empty, expected to contain %q", key, substr)
		return
	}
	// Check if substr is in the comma-separated list
	found := false
	for _, part := range splitMiddlewares(got) {
		if part == substr {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("label %q = %q, expected to contain %q", key, got, substr)
	}
}

func splitMiddlewares(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			result = append(result, p)
		}
	}
	return result
}
