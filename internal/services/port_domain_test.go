package services

import (
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

const minioConsoleRouter = "goforge-svc-00000000-0000-0000-0000-000000000042-p9001"

// renderMinio renders the repo minio template with domain and consoleDomain.
func renderMinio(t *testing.T, domain, consoleDomain string, cloudflare bool) (*containerSpec, error) {
	t.Helper()
	c := loadRepoCatalog(t)
	svc := NewInstanceService(nil, nil, nil, c, "traefik", cloudflare, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tmpl, err := c.GetTemplate("minio")
	if err != nil {
		t.Fatal(err)
	}
	config := goldenConfig(tmpl, domain)
	config["console_domain"] = consoleDomain
	if err = normalizeConfig(tmpl, config); err != nil {
		t.Fatal(err)
	}
	inst := &models.ServiceInstance{ID: "00000000-0000-0000-0000-000000000042"}
	inst.Version.String, inst.Version.Valid = tmpl.DefaultVersion, true
	inst.Domain.String, inst.Domain.Valid = domain, domain != ""
	return svc.buildContainerSpec(tmpl, inst, config)
}

// TestMinioTemplate_ConsoleDomain: a Console Domain routes a second host to
// the console port and sets MINIO_BROWSER_REDIRECT_URL, while the API host
// stays on 9000. Both routers name their service.
func TestMinioTemplate_ConsoleDomain(t *testing.T) {
	t.Parallel()
	spec, err := renderMinio(t, "minio-app.example.com", "minio-console-app.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	api := "goforge-svc-00000000-0000-0000-0000-000000000042"
	want := map[string]string{
		"traefik.http.routers." + api + ".rule":                                     "Host(`minio-app.example.com`)",
		"traefik.http.routers." + api + ".service":                                  api,
		"traefik.http.services." + api + ".loadbalancer.server.port":                "9000",
		"traefik.http.routers." + minioConsoleRouter + ".rule":                      "Host(`minio-console-app.example.com`)",
		"traefik.http.routers." + minioConsoleRouter + ".entrypoints":               "web",
		"traefik.http.routers." + minioConsoleRouter + ".service":                   minioConsoleRouter,
		"traefik.http.services." + minioConsoleRouter + ".loadbalancer.server.port": "9001",
	}
	for k, v := range want {
		if got := spec.opts.Labels[k]; got != v {
			t.Errorf("label %s = %q, want %q", k, got, v)
		}
	}
	if !slices.Contains(spec.opts.Env, "MINIO_BROWSER_REDIRECT_URL=https://minio-console-app.example.com") {
		t.Errorf("env missing MINIO_BROWSER_REDIRECT_URL: %v", envKeys(spec.opts.Env))
	}
}

// TestMinioTemplate_ConsoleDomainNoTLSRedirectOrphans: outside Cloudflare
// mode the -http redirect routers also name a service, or Traefik drops them
// once the container has two services.
func TestMinioTemplate_ConsoleDomainNoTLSRedirectOrphans(t *testing.T) {
	t.Parallel()
	spec, err := renderMinio(t, "minio-app.example.com", "minio-console-app.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.opts.Labels["traefik.http.routers."+minioConsoleRouter+"-http.service"]; got != minioConsoleRouter {
		t.Errorf("console -http router service = %q", got)
	}
	if got := spec.opts.Labels["traefik.http.routers."+minioConsoleRouter+".tls.certresolver"]; got != "letsencrypt" {
		t.Errorf("console certresolver = %q", got)
	}
}

// TestMinioTemplate_ConsoleDomainOff: no Console Domain, or no Domain at all,
// adds no console route and no redirect URL.
func TestMinioTemplate_ConsoleDomainOff(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ domain, console string }{
		{"minio-app.example.com", ""},
		{"", "minio-console-app.example.com"},
	} {
		spec, err := renderMinio(t, tc.domain, tc.console, true)
		if err != nil {
			t.Fatal(err)
		}
		for k := range spec.opts.Labels {
			if k == "traefik.http.routers."+minioConsoleRouter+".rule" {
				t.Errorf("%+v: unexpected console router", tc)
			}
		}
		for _, k := range envKeys(spec.opts.Env) {
			if k == "MINIO_BROWSER_REDIRECT_URL" {
				t.Errorf("%+v: unexpected MINIO_BROWSER_REDIRECT_URL", tc)
			}
		}
	}
}

// TestMinioTemplate_ConsoleDomainInvalid: a console domain that is not a
// hostname, or that repeats the API domain, is refused before any container
// is touched.
func TestMinioTemplate_ConsoleDomainInvalid(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"evil`) || Host(`x", "has space.example.com", "minio-app.example.com", "MINIO-APP.example.com"} {
		if _, err := renderMinio(t, "minio-app.example.com", bad, true); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("console_domain %q: err = %v, want ErrInvalidConfig", bad, err)
		}
	}
}

// TestValidatePorts_DomainOnFirstPort: the first port is the instance Domain's.
func TestValidatePorts_DomainOnFirstPort(t *testing.T) {
	t.Parallel()
	tmpl := &ServiceTemplate{Docker: DockerConfig{Ports: []PortConfig{{Container: 80, Domain: "x"}}}}
	if err := tmpl.validatePorts(); err == nil {
		t.Error("want error for domain on the first port")
	}
	tmpl.Docker.Ports = []PortConfig{{Container: 80}, {Container: 81, Domain: "{{ .Broken"}}
	if err := tmpl.validatePorts(); err == nil {
		t.Error("want error for a bad domain template")
	}
}

func envKeys(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		out = append(out, k)
	}
	return out
}
