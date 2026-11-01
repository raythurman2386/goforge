package services

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

const templateNameGiteaRunner = "gitea-runner"

// giteaRunnerScript returns the shell script the gitea-runner entrypoint runs
// under tini, failing unless the entrypoint keeps tini as PID 1.
func giteaRunnerScript(t *testing.T, entrypoint []string) string {
	t.Helper()
	if len(entrypoint) != 5 || !slices.Equal(entrypoint[:4], []string{"/sbin/tini", "--", "sh", "-c"}) {
		t.Fatalf("Entrypoint = %q, want [/sbin/tini -- sh -c <script>]", entrypoint)
	}
	return entrypoint[4]
}

// TestDeployGiteaRunner_Spec: the runner is the official act_runner image,
// keeps tini as PID 1, hands over to the image's run.sh, gets the token only
// through the environment, publishes nothing, has no Traefik router, and keeps
// its registration and caches on two named volumes.
func TestDeployGiteaRunner_Spec(t *testing.T) {
	t.Parallel()
	const token = "reg-token-1234567890"
	opts := deployRepoTemplate(t, templateNameGiteaRunner, true, map[string]interface{}{
		"name":               "runner",
		"instance_url":       "https://gitea-app.example.com",
		"registration_token": token,
	})

	if opts.Image != "gitea/act_runner:0.6.1" {
		t.Errorf("Image = %q, want gitea/act_runner:0.6.1", opts.Image)
	}
	if opts.Cmd != nil {
		t.Errorf("Cmd = %q, want nil", opts.Cmd)
	}
	script := giteaRunnerScript(t, opts.Entrypoint)
	if !strings.HasSuffix(strings.TrimSpace(script), "exec run.sh") {
		t.Error("entrypoint script does not end with exec run.sh")
	}
	if strings.Contains(strings.Join(opts.Entrypoint, " "), token) {
		t.Error("registration token rendered into the entrypoint")
	}

	env := envMap(opts)
	want := map[string]string{
		"GITEA_INSTANCE_URL":              "https://gitea-app.example.com",
		"GITEA_RUNNER_REGISTRATION_TOKEN": token,
		"GITEA_RUNNER_NAME":               "goforge-runner",
		"RUNNER_LABELS":                   "linux,alpine,python,node,typescript,go,rust",
		"RUNNER_CAPACITY":                 "1",
		"RUNNER_NPM_GLOBALS":              "typescript",
		"GOPATH":                          "/cache/go",
		"CARGO_HOME":                      "/cache/cargo",
	}
	for k, v := range want {
		if got := env[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	if len(opts.PortMappings) != 0 {
		t.Errorf("PortMappings = %v, want none", opts.PortMappings)
	}
	if hasRouter(opts.Labels) || opts.Labels["traefik.enable"] != "false" {
		t.Errorf("labels = %v, want traefik.enable=false and no router", opts.Labels)
	}
	if len(opts.Binds) != 2 || !strings.HasSuffix(opts.Binds[0], ":/data") || !strings.HasSuffix(opts.Binds[1], ":/cache") {
		t.Errorf("Binds = %v, want named volumes on /data and /cache", opts.Binds)
	}
	for _, b := range opts.Binds {
		if strings.Contains(b, "docker.sock") {
			t.Errorf("bind %q mounts the Docker socket", b)
		}
	}
	if opts.HealthCheck == nil || !strings.Contains(strings.Join(opts.HealthCheck.Test, " "), "127.0.0.1:9101/healthz") {
		t.Errorf("HealthCheck = %v, want the act_runner /healthz probe", opts.HealthCheck)
	}
}

// TestDeployGiteaRunner_BlankTokenLeftOut: an edit that leaves the token blank
// (the runner is already registered) keeps the variable out of the container.
func TestDeployGiteaRunner_BlankTokenLeftOut(t *testing.T) {
	t.Parallel()
	opts := deployRepoTemplate(t, templateNameGiteaRunner, false, map[string]interface{}{
		"name":         "runner",
		"instance_url": "https://gitea-app.example.com",
	})
	if _, ok := envMap(opts)["GITEA_RUNNER_REGISTRATION_TOKEN"]; ok {
		t.Error("GITEA_RUNNER_REGISTRATION_TOKEN set from a blank field")
	}
}

// TestGiteaRunnerTemplate_Toolchains: the default labels advertise Python,
// Node, TypeScript, Go, and Rust, and the script installs each toolchain,
// writes every label as a host label into the config act_runner reads, and
// refuses docker:// labels.
func TestGiteaRunnerTemplate_Toolchains(t *testing.T) {
	t.Parallel()
	tmpl, err := loadRepoCatalog(t).GetTemplate(templateNameGiteaRunner)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Web || tmpl.Category != "devtools" || len(tmpl.Docker.Ports) != 0 {
		t.Errorf("web = %v, category = %q, ports = %v; want a non-web devtools service without ports", tmpl.Web, tmpl.Category, tmpl.Docker.Ports)
	}
	if !containsString(tmpl.Versions, tmpl.DefaultVersion) {
		t.Errorf("default_version %q not in versions %v", tmpl.DefaultVersion, tmpl.Versions)
	}

	defaults := map[string]string{}
	for _, f := range tmpl.Config {
		if f.Default != nil {
			defaults[f.Name] = fmt.Sprintf("%v", f.Default)
		}
		if f.Name == "registration_token" && f.Type != FieldTypePassword {
			t.Errorf("registration_token type = %q, want password", f.Type)
		}
	}
	labels := strings.Split(defaults["labels"], ",")
	for _, lang := range []string{"python", "node", "typescript", "go", "rust"} {
		if !slices.Contains(labels, lang) {
			t.Errorf("default labels %q missing %s", defaults["labels"], lang)
		}
	}
	if defaults["npm_globals"] != "typescript" {
		t.Errorf("npm_globals default = %q, want typescript", defaults["npm_globals"])
	}

	script := giteaRunnerScript(t, tmpl.Docker.Entrypoint)
	for _, want := range []string{
		" python3 ", " py3-pip ", " nodejs ", " npm ", " go ", " rust ", " cargo ",
		`labels="${labels:+$labels,}$name:host"`,
		`*:*) die "label $l: only host labels work here`,
		`echo "  docker_host: \"-\""`,
		`echo "    - \"$l\""`,
		"export CONFIG_FILE=/data/config.yaml",
		`! grep -qF "\"address\": \"$GITEA_INSTANCE_URL\"" .runner`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("entrypoint script missing %q", want)
		}
	}
	if strings.Contains(script, "{{") {
		t.Error("entrypoint script contains a Go template action; pass values through the environment")
	}
}

// TestBuildContainerSpec_EntrypointRendered: docker.entrypoint elements render
// as templates, a template without one keeps the image ENTRYPOINT, and a bad
// template is an error naming the field.
func TestBuildContainerSpec_EntrypointRendered(t *testing.T) {
	t.Parallel()
	c := &Catalog{templates: map[string]*ServiceTemplate{}}
	svc := NewInstanceService(nil, nil, nil, c, "traefik", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	inst := &models.ServiceInstance{ID: "id-1"}
	inst.Version.String, inst.Version.Valid = "1", true

	tmpl := &ServiceTemplate{Name: "t", Config: []ConfigField{{Name: "mode", Type: "text"}}, Docker: DockerConfig{Image: "img:{{ .Version }}", Entrypoint: []string{"/bin/app", "--mode={{ .Config.mode }}"}}}
	spec, err := svc.buildContainerSpec(tmpl, inst, map[string]interface{}{"mode": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spec.opts.Entrypoint, " "); got != "/bin/app --mode=x" {
		t.Errorf("Entrypoint = %q, want %q", got, "/bin/app --mode=x")
	}

	tmpl.Docker.Entrypoint = nil
	if spec, err = svc.buildContainerSpec(tmpl, inst, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}
	if spec.opts.Entrypoint != nil {
		t.Errorf("Entrypoint = %q, want nil (image default)", spec.opts.Entrypoint)
	}

	tmpl.Docker.Entrypoint = []string{"{{ .Broken"}
	if _, err = svc.buildContainerSpec(tmpl, inst, map[string]interface{}{}); err == nil || !strings.Contains(err.Error(), "entrypoint[0]") {
		t.Errorf("err = %v, want an entrypoint[0] interpolation error", err)
	}
}
