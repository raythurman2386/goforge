package services

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

// renderRepoSpec renders the repo template name with its golden config and
// domain, the way a deploy would.
func renderRepoSpec(t *testing.T, name, domain string) *containerSpec {
	t.Helper()
	c := loadRepoCatalog(t)
	svc := NewInstanceService(nil, nil, nil, c, "traefik", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tmpl, err := c.GetTemplate(name)
	if err != nil {
		t.Fatal(err)
	}
	config := goldenConfig(tmpl, domain)
	if err = normalizeConfig(tmpl, config); err != nil {
		t.Fatal(err)
	}
	inst := &models.ServiceInstance{ID: "00000000-0000-0000-0000-000000000042"}
	inst.Version.String, inst.Version.Valid = tmpl.DefaultVersion, true
	inst.Domain.String, inst.Domain.Valid = domain, domain != ""
	spec, err := svc.buildContainerSpec(tmpl, inst, config)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func hasRouter(labels map[string]string) bool {
	for k := range labels {
		if strings.HasPrefix(k, "traefik.http.routers.") {
			return true
		}
	}
	return false
}

// TestTraefikLabels_DataServicesDisabled: postgres and redis deployed without
// a domain get traefik.enable=false and no router, while the live web
// services (they always have a domain) keep traefik.enable=true and a router.
func TestTraefikLabels_DataServicesDisabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		template   string
		domain     string
		wantEnable string
		wantRouter bool
	}{
		{template: templateNamePostgres, wantEnable: "false"},
		{template: templateNameRedis, wantEnable: "false"},
		{template: "gitea", domain: "gitea-app.example.com", wantEnable: "true", wantRouter: true},
		{template: "vaultwarden", domain: "vaultwarden-app.example.com", wantEnable: "true", wantRouter: true},
		{template: "solidcraft", domain: "solidcraft-app.example.com", wantEnable: "true", wantRouter: true},
	}
	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			t.Parallel()
			labels := renderRepoSpec(t, tt.template, tt.domain).opts.Labels
			if got := labels["traefik.enable"]; got != tt.wantEnable {
				t.Errorf("traefik.enable = %q, want %q", got, tt.wantEnable)
			}
			if got := hasRouter(labels); got != tt.wantRouter {
				t.Errorf("has router = %v, want %v (labels %v)", got, tt.wantRouter, labels)
			}
			if labels["goforge.managed"] != "true" {
				t.Errorf("goforge.managed = %q, want true", labels["goforge.managed"])
			}
		})
	}
}

// TestRedisTemplate_EnforcesAuth: the redis template sets a command that
// starts redis-server with requirepass, keeps the password out of the
// command and health check argv, and authenticates its health check.
func TestRedisTemplate_EnforcesAuth(t *testing.T) {
	t.Parallel()
	spec := renderRepoSpec(t, templateNameRedis, "")
	const password = "fixed-password"

	cmd := spec.opts.Cmd
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" {
		t.Fatalf("Cmd = %q, want [sh -c <script>]", cmd)
	}
	script := cmd[2]
	for _, want := range []string{"requirepass", "$REDIS_PASSWORD", "exec docker-entrypoint.sh redis-server"} {
		if !strings.Contains(script, want) {
			t.Errorf("command script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(strings.Join(cmd, " "), password) {
		t.Error("password rendered into the container command")
	}

	if !containsString(spec.opts.Env, "REDIS_PASSWORD="+password) {
		t.Errorf("Env = %q, want REDIS_PASSWORD set", spec.opts.Env)
	}

	hc := strings.Join(spec.opts.HealthCheck.Test, " ")
	if !strings.Contains(hc, "REDISCLI_AUTH=") {
		t.Errorf("health check %q does not authenticate", hc)
	}
	if strings.Contains(hc, password) {
		t.Error("password rendered into the health check")
	}
}

// TestBuildContainerSpec_CommandRendered: docker.command elements render as
// templates, and a template without a command keeps the image CMD.
func TestBuildContainerSpec_CommandRendered(t *testing.T) {
	t.Parallel()
	c := &Catalog{templates: map[string]*ServiceTemplate{}}
	svc := NewInstanceService(nil, nil, nil, c, "traefik", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	inst := &models.ServiceInstance{ID: "id-1"}
	inst.Version.String, inst.Version.Valid = "7", true

	tmpl := &ServiceTemplate{Name: "t", Config: []ConfigField{{Name: "label", Type: "text"}}, Docker: DockerConfig{Image: "img:{{ .Version }}", Command: []string{"server", "--name={{ .Config.label }}"}}}
	spec, err := svc.buildContainerSpec(tmpl, inst, map[string]interface{}{"label": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spec.opts.Cmd, " "); got != "server --name=x" {
		t.Errorf("Cmd = %q, want %q", got, "server --name=x")
	}

	tmpl.Docker.Command = nil
	if spec, err = svc.buildContainerSpec(tmpl, inst, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if spec.opts.Cmd != nil {
		t.Errorf("Cmd = %q, want nil (image default)", spec.opts.Cmd)
	}

	tmpl.Docker.Command = []string{"{{ .Broken"}
	if _, err = svc.buildContainerSpec(tmpl, inst, map[string]interface{}{}); err == nil {
		t.Error("want error for a bad command template")
	}
}

// TestMinioTemplate_Command: minio's command, a string GoForge used to
// ignore, is now a list applied as the container CMD.
func TestMinioTemplate_Command(t *testing.T) {
	t.Parallel()
	got := strings.Join(renderRepoSpec(t, "minio", "minio-app.example.com").opts.Cmd, " ")
	if want := "server /data --console-address :9001"; got != want {
		t.Errorf("Cmd = %q, want %q", got, want)
	}
}
