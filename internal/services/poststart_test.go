package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// hookHarness runs InstanceService against the repo catalog with in-memory
// repos and a Docker fake that records creates, health waits, and execs.
type hookHarness struct {
	mu        sync.Mutex
	instance  *models.ServiceInstance
	container *models.Container
	created   []docker.ContainerOptions
	waits     []string
	execs     []docker.ExecOptions
	execRes   docker.ExecResult
	execErr   error
	waitErr   error
	done      chan models.ServiceInstanceStatus
	logs      *syncBuffer
	svc       *InstanceService
}

func newHookHarness(t *testing.T) *hookHarness {
	t.Helper()
	h := &hookHarness{done: make(chan models.ServiceInstanceStatus, 4), logs: &syncBuffer{}}
	ir := &mockInstanceRepository{
		createFunc: func(_ context.Context, s *models.ServiceInstance) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			s.ID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			cp := *s
			h.instance = &cp
			return nil
		},
		getByIDFunc: func(_ context.Context, _ string) (*models.ServiceInstance, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			cp := *h.instance
			return &cp, nil
		},
		updateFunc: func(_ context.Context, s *models.ServiceInstance) error {
			h.mu.Lock()
			cp := *s
			h.instance = &cp
			h.mu.Unlock()
			if s.Status == models.ServiceStatusRunning || s.Status == models.ServiceStatusFailed {
				h.done <- s.Status
			}
			return nil
		},
	}
	cr := &mockContainerRepository{
		createFunc: func(_ context.Context, c *models.Container) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			cp := *c
			cp.ID = "row-" + c.DockerID
			h.container = &cp
			return nil
		},
		getByServiceInstanceIDFunc: func(_ context.Context, _ string) (*models.Container, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.container == nil {
				return nil, errors.New("not found")
			}
			cp := *h.container
			return &cp, nil
		},
	}
	dc := &mockDockerClient{
		createContainerFunc: func(_ context.Context, opts docker.ContainerOptions) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.created = append(h.created, opts)
			return "docker-" + string(rune('0'+len(h.created))), nil
		},
		waitHealthyFunc: func(_ context.Context, id string, _ time.Duration) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.waits = append(h.waits, id)
			return h.waitErr
		},
		execFunc: func(_ context.Context, _ string, opts docker.ExecOptions) (docker.ExecResult, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.execs = append(h.execs, opts)
			return h.execRes, h.execErr
		},
	}
	logger := slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h.svc = NewInstanceService(ir, cr, dc, loadRepoCatalog(t), "traefik", true, logger)
	return h
}

func (h *hookHarness) wait(t *testing.T) models.ServiceInstanceStatus {
	t.Helper()
	select {
	case st := <-h.done:
		return st
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for background container work")
		return ""
	}
}

func (h *hookHarness) storedConfig(t *testing.T) map[string]interface{} {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	cfg, err := h.instance.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func execEnv(opts docker.ExecOptions) map[string]string {
	return envMap(docker.ContainerOptions{Env: opts.Env})
}

func giteaConfig(domain string) map[string]interface{} {
	return map[string]interface{}{
		"name":                 "git",
		"domain":               domain,
		"app_name":             "Example Git",
		"disable_registration": "true",
		"require_signin_view":  "true",
		"admin_username":       "gitea-admin",
		"admin_email":          "",
	}
}

func TestDeployGitea_LocksInstallAndRunsAdminHook(t *testing.T) {
	t.Parallel()
	h := newHookHarness(t)
	domain := "git-app.example.com"
	if _, err := h.svc.DeployInstance(context.Background(), "u1", "gitea", giteaConfig(domain)); err != nil {
		t.Fatalf("DeployInstance: %v", err)
	}
	if st := h.wait(t); st != models.ServiceStatusRunning {
		t.Fatalf("status = %s, want running", st)
	}

	cfg := h.storedConfig(t)
	password, _ := cfg["admin_password"].(string)
	secretKey, _ := cfg["secret_key"].(string)
	if len(password) < 32 {
		t.Errorf("generated admin_password has %d chars, want >= 32", len(password))
	}
	if len(secretKey) < 64 {
		t.Errorf("generated secret_key has %d chars, want >= 64", len(secretKey))
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	env := envMap(h.created[0])
	if env["GITEA__security__INSTALL_LOCK"] != "true" {
		t.Errorf("INSTALL_LOCK = %q, want true", env["GITEA__security__INSTALL_LOCK"])
	}
	if env["GITEA__security__SECRET_KEY"] != secretKey {
		t.Error("GITEA__security__SECRET_KEY does not match the stored secret_key")
	}
	for k, v := range env {
		if strings.Contains(v, password) {
			t.Errorf("container env %s carries the admin password; it belongs only in the hook env", k)
		}
	}

	if len(h.waits) != 1 || h.waits[0] != "docker-1" {
		t.Errorf("health waits = %v, want one on docker-1", h.waits)
	}
	if len(h.execs) != 1 {
		t.Fatalf("execs = %d, want 1", len(h.execs))
	}
	hook := h.execs[0]
	if hook.User != "git" {
		t.Errorf("hook user = %q, want git", hook.User)
	}
	if strings.Contains(strings.Join(hook.Cmd, " "), password) {
		t.Error("hook command contains the admin password; pass it through env")
	}
	want := map[string]string{
		"GITEA_ADMIN_USERNAME": "gitea-admin",
		"GITEA_ADMIN_EMAIL":    "admin@" + domain,
		"GITEA_ADMIN_PASSWORD": password,
	}
	got := execEnv(hook)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("hook env %s mismatch (got %d chars, want %d)", k, len(got[k]), len(v))
		}
	}
	if h.logs.contains(password) || h.logs.contains(secretKey) {
		t.Error("logs contain the admin password or secret key")
	}
}

func TestDeployGitea_AdminEmailFallbacks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		domain string
		email  string
		want   func(container string) string
	}{
		{name: "explicit email wins", domain: "git.example.com", email: "me@example.org", want: func(string) string { return "me@example.org" }},
		{name: "no domain uses container name", want: func(c string) string { return "admin@" + c }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHookHarness(t)
			cfg := giteaConfig(tt.domain)
			cfg["admin_email"] = tt.email
			if _, err := h.svc.DeployInstance(context.Background(), "u1", "gitea", cfg); err != nil {
				t.Fatal(err)
			}
			h.wait(t)
			h.mu.Lock()
			defer h.mu.Unlock()
			if got, want := execEnv(h.execs[0])["GITEA_ADMIN_EMAIL"], tt.want(h.created[0].Name); got != want {
				t.Errorf("GITEA_ADMIN_EMAIL = %q, want %q", got, want)
			}
		})
	}
}

func TestPostStart_FailedHookMarksInstanceFailed(t *testing.T) {
	t.Parallel()
	h := newHookHarness(t)
	h.execRes = docker.ExecResult{ExitCode: 1}
	if _, err := h.svc.DeployInstance(context.Background(), "u1", "gitea", giteaConfig("")); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusFailed {
		t.Fatalf("status = %s, want failed", st)
	}
	password, _ := h.storedConfig(t)["admin_password"].(string)
	// Echo the secret back in the hook output: the log must mask it.
	h.mu.Lock()
	h.execRes = docker.ExecResult{ExitCode: 2, Output: "bad password " + password}
	h.mu.Unlock()
	if _, err := h.svc.UpdateInstanceImage(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusFailed {
		t.Fatalf("status after update = %s, want failed", st)
	}

	recs := h.logs.records(t)
	var failures []map[string]any
	for _, r := range recs {
		if r["msg"] == "post_start hook failed" {
			failures = append(failures, r)
		}
	}
	if len(failures) != 2 {
		t.Fatalf("post_start hook failed logs = %d, want 2", len(failures))
	}
	if failures[1]["exit_code"] != float64(2) || failures[1]["hook"] != "create-admin" || failures[1]["duration"] == nil {
		t.Errorf("failure log = %v, want hook, exit_code 2, and duration", failures[1])
	}
	if out, _ := failures[1]["output"].(string); !strings.Contains(out, MaskedValue) {
		t.Errorf("failure output = %q, want the password masked", out)
	}
	if h.logs.contains(password) {
		t.Error("logs contain the admin password")
	}
}

func TestPostStart_UnhealthyContainerSkipsHooksAndFails(t *testing.T) {
	t.Parallel()
	h := newHookHarness(t)
	h.waitErr = errors.New("container health check reports unhealthy")
	if _, err := h.svc.DeployInstance(context.Background(), "u1", "gitea", giteaConfig("")); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusFailed {
		t.Fatalf("status = %s, want failed", st)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.execs) != 0 {
		t.Errorf("execs = %d, want 0 when the container never got healthy", len(h.execs))
	}
}

func TestPostStart_TemplateWithoutHooksNeverExecs(t *testing.T) {
	t.Parallel()
	h := newHookHarness(t)
	if _, err := h.svc.DeployInstance(context.Background(), "u1", "vaultwarden", map[string]interface{}{
		"name": "vw", "domain": "vw.example.com", "signups_allowed": "true",
	}); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusRunning {
		t.Fatalf("status = %s, want running", st)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.waits) != 0 || len(h.execs) != 0 {
		t.Errorf("waits = %d, execs = %d; want 0 and 0 for a template without post_start", len(h.waits), len(h.execs))
	}
}

func TestPostStart_RecreateRunsHookAgainAndKeepsImmutableValues(t *testing.T) {
	t.Parallel()
	h := newHookHarness(t)
	if _, err := h.svc.DeployInstance(context.Background(), "u1", "gitea", giteaConfig("git.example.com")); err != nil {
		t.Fatal(err)
	}
	h.wait(t)
	before := h.storedConfig(t)

	// An edit cannot change immutable fields, even when the form sends them.
	if _, err := h.svc.UpdateInstanceConfig(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", map[string]interface{}{
		"name": "git", "domain": "git.example.com", "app_name": "Renamed",
		"disable_registration": "true", "require_signin_view": "false",
		"admin_username": "gitea-admin", "admin_password": "typed-new-password", "secret_key": "typed-new-key",
	}); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusRunning {
		t.Fatalf("status after edit = %s, want running", st)
	}
	if _, err := h.svc.UpdateInstanceImage(context.Background(), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusRunning {
		t.Fatalf("status after image update = %s, want running", st)
	}

	after := h.storedConfig(t)
	for _, k := range []string{"admin_password", "secret_key"} {
		if after[k] != before[k] {
			t.Errorf("%s changed on edit; immutable fields must keep their deploy value", k)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.execs) != 3 || len(h.waits) != 3 {
		t.Errorf("execs = %d, waits = %d; want 3 each (deploy, edit, update)", len(h.execs), len(h.waits))
	}
	if got := envMap(h.created[2])["GITEA__security__SECRET_KEY"]; got != before["secret_key"] {
		t.Error("recreated container got a different SECRET_KEY")
	}
}

// An instance deployed before admin_password and secret_key existed must not
// get a SECRET_KEY on edit: Gitea already wrote its own key to app.ini, and
// replacing it would break everything encrypted with it.
func TestPostStart_LegacyGiteaEditKeepsExistingSecretKey(t *testing.T) {
	t.Parallel()
	h := newHookHarness(t)
	legacy := map[string]interface{}{
		"name": "git", "domain": "git.example.com", "app_name": "Old",
		"disable_registration": "true", "require_signin_view": "true",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	h.instance = &models.ServiceInstance{
		ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", UserID: "u1", TemplateName: "gitea", Name: "git",
		Version: sql.NullString{String: "1.25.5", Valid: true}, Config: raw,
		Domain: sql.NullString{String: "git.example.com", Valid: true}, Status: models.ServiceStatusRunning,
	}
	h.container = &models.Container{ID: "row-old", DockerID: "docker-old"}

	if _, err := h.svc.UpdateInstanceConfig(context.Background(), h.instance.ID, map[string]interface{}{
		"name": "git", "domain": "git.example.com", "app_name": "Old",
		"disable_registration": "true", "require_signin_view": "true", "admin_username": "gitea-admin",
	}); err != nil {
		t.Fatal(err)
	}
	if st := h.wait(t); st != models.ServiceStatusRunning {
		t.Fatalf("status = %s, want running", st)
	}
	cfg := h.storedConfig(t)
	if _, ok := cfg["secret_key"]; ok {
		t.Error("edit generated a secret_key for a legacy instance")
	}
	if _, ok := cfg["admin_password"]; ok {
		t.Error("edit generated an admin_password for a legacy instance")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	env := envMap(h.created[0])
	if _, ok := env["GITEA__security__SECRET_KEY"]; ok {
		t.Error("legacy instance container got GITEA__security__SECRET_KEY; it would overwrite app.ini")
	}
	for k, v := range env {
		if strings.Contains(v, "<no value>") {
			t.Errorf("env %s rendered <no value>", k)
		}
	}
	if got := execEnv(h.execs[0])["GITEA_ADMIN_PASSWORD"]; got != "" {
		t.Errorf("legacy hook password = %d chars, want empty (the hook exits 0 when an admin exists)", len(got))
	}
}

func TestCatalog_RejectsPostStartWithoutCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := "name: broken\nversions: [\"1\"]\ndefault_version: \"1\"\ndocker:\n  image: x\npost_start:\n  - name: nothing\n"
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewCatalog(dir)
	if err := c.LoadTemplates(); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Errorf("LoadTemplates error = %v, want command is required", err)
	}
}
