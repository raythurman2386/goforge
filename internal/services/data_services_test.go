package services

import (
	"io"
	"log/slog"
	"os"
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
// starts redis-server with an ACL file whose default user carries the
// SHA-256 of REDIS_PASSWORD (not requirepass, which an ACL file resets),
// refuses an empty password, keeps the password out of the command and
// health check argv, and authenticates its health check.
func TestRedisTemplate_EnforcesAuth(t *testing.T) {
	t.Parallel()
	spec := renderRepoSpec(t, templateNameRedis, "")
	const password = "fixed-password"

	cmd := spec.opts.Cmd
	if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" {
		t.Fatalf("Cmd = %q, want [sh -c <script>]", cmd)
	}
	script := cmd[2]
	for _, want := range []string{
		`if [ -z "${REDIS_PASSWORD:-}" ]; then`,
		`printf '%s' "$REDIS_PASSWORD" | sha256sum`,
		`printf 'user default on #%s ~* &* +@all\n' "$hash"`,
		`acl=/data/users.acl`,
		`"aclfile $acl"`,
		`grep -v '^user default ' "$acl"`,
		`mv -f "$next" "$acl"`,
		"exec docker-entrypoint.sh redis-server",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("command script missing %q:\n%s", want, script)
		}
	}
	for _, bad := range []string{"requirepass", "nopass", ">$REDIS_PASSWORD", "/tmp/users.acl"} {
		if strings.Contains(script, bad) {
			t.Errorf("command script contains %q:\n%s", bad, script)
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

// TestRedisTenantScript_Rules: redis-tenant.sh refuses instances without an
// ACL file before any change, resets channels (6.2 keeps allchannels
// otherwise), denies name-listing commands, and denies FUNCTION only on 7+
// (6.2 rejects the unknown command).
func TestRedisTenantScript_Rules(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../scripts/shared-data/redis-tenant.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	for _, want := range []string{
		`cli config get aclfile`,
		`~$APP:* resetchannels &$APP:* +@all -@admin -@dangerous +info -scan -randomkey -pubsub"`,
		`if [ "$major" -ge 7 ]; then rules="$rules -function"; fi`,
		`cli acl load`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("redis-tenant.sh missing %q", want)
		}
	}
	guard := strings.Index(script, "cli config get aclfile")
	if set := strings.Index(script, "ACL SETUSER $APP"); guard < 0 || set < guard {
		t.Error("ACL SETUSER comes before the aclfile guard")
	}
	if del := strings.Index(script, "ACL DELUSER $APP"); guard < 0 || del < guard {
		t.Error("ACL DELUSER comes before the aclfile guard")
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

// TestRedisTemplate_SessionsAndQueues: Redis is for sessions and queues, so
// the conf turns AOF on, caps memory below the container limit, and refuses
// writes when full instead of evicting. Every setting, including the ACL
// file under /data, is written into the per-start conf file.
func TestRedisTemplate_SessionsAndQueues(t *testing.T) {
	t.Parallel()
	spec := renderRepoSpec(t, templateNameRedis, "")
	if spec.opts.Image != "redis:7.4" {
		t.Errorf("Image = %q, want redis:7.4", spec.opts.Image)
	}
	script := spec.opts.Cmd[2]
	for _, want := range []string{"'appendonly yes'", "'appendfsync everysec'", "'maxmemory 256mb'", "'maxmemory-policy noeviction'", "'dir /data'", `rm -f "$conf"`} {
		if !strings.Contains(script, want) {
			t.Errorf("command script missing %s:\n%s", want, script)
		}
	}
	const maxmemory = 256 << 20
	if spec.opts.Memory < 2*maxmemory {
		t.Errorf("container memory %d, want at least twice maxmemory (%d) for AOF rewrite headroom", spec.opts.Memory, 2*maxmemory)
	}
}

// TestPostgresTemplate_SharedDefaults: Postgres 17 by default, a pgadmin
// admin account, and the init-only settings (database, username, password)
// are immutable so a config edit cannot point connection_info at an account
// the data volume never created.
func TestPostgresTemplate_SharedDefaults(t *testing.T) {
	t.Parallel()
	spec := renderRepoSpec(t, templateNamePostgres, "")
	if spec.opts.Image != "postgres:17" {
		t.Errorf("Image = %q, want postgres:17", spec.opts.Image)
	}
	for _, want := range []string{"POSTGRES_USER=pgadmin", "POSTGRES_DB=app", "POSTGRES_PASSWORD=fixed-password"} {
		if !containsString(spec.opts.Env, want) {
			t.Errorf("Env = %q, want %s", spec.opts.Env, want)
		}
	}
	if spec.opts.NanoCPUs != 1_000_000_000 || spec.opts.Memory != 1<<30 {
		t.Errorf("resources = %d NanoCPUs, %d bytes, want 1 CPU, 1GiB", spec.opts.NanoCPUs, spec.opts.Memory)
	}
	hc := strings.Join(spec.opts.HealthCheck.Test, " ")
	if !strings.Contains(hc, `"$POSTGRES_USER"`) || !strings.Contains(hc, `"$POSTGRES_DB"`) {
		t.Errorf("health check %q, want pg_isready with $POSTGRES_USER and $POSTGRES_DB", hc)
	}

	tmpl, err := loadRepoCatalog(t).GetTemplate(templateNamePostgres)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]ConfigField{}
	for _, f := range tmpl.Config {
		fields[f.Name] = f
	}
	for _, name := range []string{"database", "username", "password"} {
		if !fields[name].Immutable {
			t.Errorf("field %s is not immutable", name)
		}
	}
	if pw := fields["password"]; !pw.Generate || pw.Length < 32 {
		t.Errorf("password field generate=%v length=%d, want generated, length >= 32", pw.Generate, pw.Length)
	}
	if !containsString(tmpl.Versions, "17") || tmpl.DefaultVersion != "17" {
		t.Errorf("versions %v default %q, want 17 offered and default", tmpl.Versions, tmpl.DefaultVersion)
	}
}

// TestPostgresTemplate_EditKeepsInitSettings: a config edit cannot change the
// immutable database, username, or password of an existing instance.
func TestPostgresTemplate_EditKeepsInitSettings(t *testing.T) {
	t.Parallel()
	tmpl, err := loadRepoCatalog(t).GetTemplate(templateNamePostgres)
	if err != nil {
		t.Fatal(err)
	}
	current := map[string]interface{}{"name": "shared-pg", "database": "app", "username": "pgadmin", "password": "orig"}
	merged := mergeEditedConfig(tmpl, current, map[string]interface{}{"database": "other", "username": "postgres", "password": "changed"})
	for k, want := range map[string]string{"database": "app", "username": "pgadmin", "password": "orig"} {
		if merged[k] != want {
			t.Errorf("%s = %v after edit, want %s", k, merged[k], want)
		}
	}
}
