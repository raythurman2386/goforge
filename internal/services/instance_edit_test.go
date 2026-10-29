package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// editHarness wires InstanceService to in-memory fakes and records docker calls.
type editHarness struct {
	mu         sync.Mutex
	instance   *models.ServiceInstance
	container  *models.Container
	created    []docker.ContainerOptions
	stopped    []string
	removed    []string
	deletedRow []string
	createErr  error
	pulled     []string
	pullErr    error
	events     []string // docker calls in order: pull, stop, remove, create
	done       chan struct{}
	svc        *InstanceService
}

func editTestTemplate() *ServiceTemplate {
	return &ServiceTemplate{
		Name:           "vaultwarden",
		Web:            true,
		DefaultVersion: "latest",
		Config: []ConfigField{
			{Name: "signups_allowed", Label: "Allow Signups", Type: FieldTypeBool, Default: true, Required: true},
			{Name: "admin_token", Type: "password", Generate: true, Length: 48, Required: true},
		},
		Resources: ResourceConfig{CPU: "0.25", Memory: "256m"},
		Docker: DockerConfig{
			Image: "vaultwarden/server:{{ .Version }}",
			Ports: []PortConfig{{Container: 80, Protocol: "tcp"}},
			Environment: map[string]string{
				"DOMAIN":          "{{ if .Domain }}{{ .Protocol }}://{{ .Domain }}{{ end }}",
				"SIGNUPS_ALLOWED": "{{ .Config.signups_allowed }}",
				"ADMIN_TOKEN":     "{{ .Config.admin_token }}",
			},
			Volumes: []VolumeConfig{
				{Name: "data", Path: "/data"},
				{Name: "attachments", Path: "/data/attachments"},
			},
		},
		Connection: ConnectionConfig{
			External: "{{ .Protocol }}://{{ .Domain }}",
		},
	}
}

func newEditHarness(t *testing.T) *editHarness {
	t.Helper()
	h := &editHarness{done: make(chan struct{}, 4)}

	catalog := NewCatalog("dummy")
	catalog.templates["vaultwarden"] = editTestTemplate()

	ir := &mockInstanceRepository{
		createFunc: func(_ context.Context, s *models.ServiceInstance) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			s.ID = "11111111-2222-3333-4444-555555555555"
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
				h.done <- struct{}{}
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
		deleteFunc: func(_ context.Context, id string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.deletedRow = append(h.deletedRow, id)
			return nil
		},
	}
	dc := &mockDockerClient{
		createContainerFunc: func(_ context.Context, opts docker.ContainerOptions) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.createErr != nil {
				return "", h.createErr
			}
			h.events = append(h.events, "create")
			h.created = append(h.created, opts)
			return "docker-" + string(rune('0'+len(h.created))), nil
		},
		stopContainerFunc: func(_ context.Context, id string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.events = append(h.events, "stop")
			h.stopped = append(h.stopped, id)
			return nil
		},
		removeContainerFunc: func(_ context.Context, id string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.events = append(h.events, "remove")
			h.removed = append(h.removed, id)
			return nil
		},
		pullImageFunc: func(_ context.Context, image string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.events = append(h.events, "pull")
			h.pulled = append(h.pulled, image)
			return h.pullErr
		},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.svc = NewInstanceService(ir, cr, dc, catalog, "goforge_net", false, logger)
	return h
}

func (h *editHarness) wait(t *testing.T) {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for background container work")
	}
}

func (h *editHarness) deploy(t *testing.T) *models.ServiceInstance {
	t.Helper()
	inst, err := h.svc.DeployInstance(context.Background(), "user-1", "vaultwarden", map[string]interface{}{
		"name":            "vw",
		"domain":          "vw.example.com",
		"signups_allowed": "true",
		"admin_token":     "original-secret",
	})
	if err != nil {
		t.Fatalf("DeployInstance: %v", err)
	}
	h.wait(t)
	return inst
}

func TestUpdateInstanceConfig_ReusesNamedVolumes(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	_, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{
		"csrf_token":      "ignored",
		"name":            "vault", // renaming must not rename volumes
		"domain":          "vault.example.com",
		"signups_allowed": "false",
		"admin_token":     "", // blank keeps the stored secret
		"unknown":         "ignored",
	})
	if err != nil {
		t.Fatalf("UpdateInstanceConfig: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.created) != 2 {
		t.Fatalf("created %d containers, want 2 (deploy + recreate)", len(h.created))
	}
	before, after := h.created[0], h.created[1]

	wantBinds := []string{
		"goforge-vol-" + inst.ID + "-data:/data",
		"goforge-vol-" + inst.ID + "-attachments:/data/attachments",
	}
	if !slices.Equal(before.Binds, wantBinds) {
		t.Fatalf("deploy binds = %v, want %v", before.Binds, wantBinds)
	}
	if !slices.Equal(after.Binds, before.Binds) {
		t.Fatalf("recreate binds = %v, want same volumes as deploy %v", after.Binds, before.Binds)
	}
	if after.Name != before.Name {
		t.Errorf("container name changed: %q -> %q", before.Name, after.Name)
	}
	if before.NanoCPUs != 250_000_000 || before.Memory != 256*1024*1024 {
		t.Errorf("deploy limits: NanoCPUs=%d Memory=%d, want 0.25 CPU and 256m", before.NanoCPUs, before.Memory)
	}
	if after.NanoCPUs != before.NanoCPUs || after.Memory != before.Memory {
		t.Errorf("recreate limits: NanoCPUs=%d Memory=%d, want same as deploy (%d, %d)", after.NanoCPUs, after.Memory, before.NanoCPUs, before.Memory)
	}

	if !slices.Equal(h.stopped, []string{"docker-1"}) || !slices.Equal(h.removed, []string{"docker-1"}) {
		t.Errorf("stopped=%v removed=%v, want old container docker-1 stopped and removed", h.stopped, h.removed)
	}
	if !slices.Equal(h.deletedRow, []string{"row-docker-1"}) {
		t.Errorf("deleted container rows = %v, want [row-docker-1]", h.deletedRow)
	}
	if h.container == nil || h.container.DockerID != "docker-2" {
		t.Errorf("container record = %+v, want docker-2", h.container)
	}

	env := envMap(after)
	if env["SIGNUPS_ALLOWED"] != "false" {
		t.Errorf("SIGNUPS_ALLOWED = %q, want false", env["SIGNUPS_ALLOWED"])
	}
	if env["ADMIN_TOKEN"] != "original-secret" {
		t.Errorf("ADMIN_TOKEN = %q, want blank password to keep original-secret", env["ADMIN_TOKEN"])
	}
	if env["DOMAIN"] != "https://vault.example.com" {
		t.Errorf("DOMAIN = %q, want https://vault.example.com", env["DOMAIN"])
	}
	foundRule := false
	for k, v := range after.Labels {
		if strings.HasSuffix(k, ".rule") {
			if strings.Contains(v, "vw.example.com") {
				t.Errorf("router %s still routes the old domain: %s", k, v)
			}
			if strings.Contains(v, "vault.example.com") {
				foundRule = true
			}
		}
	}
	if !foundRule {
		t.Errorf("no Traefik rule for the new domain in %v", after.Labels)
	}

	if h.instance.Status != models.ServiceStatusRunning {
		t.Errorf("status = %s, want running", h.instance.Status)
	}
	if h.instance.ID != inst.ID {
		t.Errorf("instance ID changed: %s -> %s", inst.ID, h.instance.ID)
	}
	if h.instance.Name != "vault" {
		t.Errorf("stored name = %q, want vault", h.instance.Name)
	}
	if h.instance.Domain.String != "vault.example.com" {
		t.Errorf("stored domain = %q", h.instance.Domain.String)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal(h.instance.Config, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["csrf_token"]; ok {
		t.Error("csrf_token leaked into stored config")
	}
	if _, ok := stored["unknown"]; ok {
		t.Error("unknown form key leaked into stored config")
	}
	if stored["admin_token"] != "original-secret" {
		t.Errorf("stored admin_token = %v", stored["admin_token"])
	}
}

func TestUpdateInstanceConfig_ReplacesFilledPassword(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	if _, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{
		"admin_token": "new-secret",
	}); err != nil {
		t.Fatalf("UpdateInstanceConfig: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	env := envMap(h.created[1])
	if env["ADMIN_TOKEN"] != "new-secret" {
		t.Errorf("ADMIN_TOKEN = %q, want new-secret", env["ADMIN_TOKEN"])
	}
	// Fields not posted keep their stored values.
	if env["SIGNUPS_ALLOWED"] != "true" || h.instance.Name != "vw" {
		t.Errorf("unposted fields changed: SIGNUPS_ALLOWED=%q name=%q", env["SIGNUPS_ALLOWED"], h.instance.Name)
	}
	if !slices.Equal(h.created[1].Binds, h.created[0].Binds) {
		t.Errorf("binds changed: %v -> %v", h.created[0].Binds, h.created[1].Binds)
	}
}

func TestUpdateInstanceConfig_RejectsWhileDeploying(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	h.instance = &models.ServiceInstance{
		ID:           "inst-busy",
		TemplateName: "vaultwarden",
		Name:         "vw",
		Status:       models.ServiceStatusPending,
		Version:      sql.NullString{String: "latest", Valid: true},
	}

	_, err := h.svc.UpdateInstanceConfig(context.Background(), "inst-busy", map[string]interface{}{"signups_allowed": "false"})
	if !errors.Is(err, ErrInstanceBusy) {
		t.Fatalf("err = %v, want ErrInstanceBusy", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created)+len(h.stopped)+len(h.removed) != 0 {
		t.Errorf("docker touched while busy: created=%d stopped=%v removed=%v", len(h.created), h.stopped, h.removed)
	}
}

func TestUpdateInstanceConfig_CreateFailureKeepsVolumesAndMarksFailed(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	h.mu.Lock()
	h.createErr = errors.New("boom")
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{"signups_allowed": "false"}); err != nil {
		t.Fatalf("UpdateInstanceConfig: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.instance.Status != models.ServiceStatusFailed {
		t.Errorf("status = %s, want failed", h.instance.Status)
	}
	if len(h.deletedRow) != 0 {
		t.Errorf("container row deleted after failed create: %v", h.deletedRow)
	}
}

func TestMergeEditedConfig(t *testing.T) {
	t.Parallel()
	tmpl := editTestTemplate()
	current := map[string]interface{}{
		"name":            "vw",
		"domain":          "vw.example.com",
		"signups_allowed": "true",
		"admin_token":     "keep-me",
		"legacy":          "stays",
	}

	got := mergeEditedConfig(tmpl, current, map[string]interface{}{
		"name":            "  ",
		"domain":          "",
		"signups_allowed": " false ",
		"admin_token":     "",
		"csrf_token":      "x",
	})

	want := map[string]interface{}{
		"name":            "vw",
		"domain":          "",
		"signups_allowed": "false",
		"admin_token":     "keep-me",
		"legacy":          "stays",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if _, ok := got["csrf_token"]; ok {
		t.Error("csrf_token should be ignored")
	}
	if current["signups_allowed"] != "true" {
		t.Error("mergeEditedConfig mutated current")
	}
}

func TestVolumeName(t *testing.T) {
	t.Parallel()
	if got := VolumeName("abc", "data"); got != "goforge-vol-abc-data" {
		t.Errorf("VolumeName = %q", got)
	}
}

func TestUpdateInstanceConfig_LogsDomainAndTemplate(t *testing.T) {
	t.Parallel()

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	h := &editHarness{done: make(chan struct{}, 4)}
	catalog := NewCatalog("dummy")
	catalog.templates["vaultwarden"] = editTestTemplate()

	ir := &mockInstanceRepository{
		createFunc: func(_ context.Context, s *models.ServiceInstance) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			s.ID = "11111111-2222-3333-4444-555555555555"
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
				h.done <- struct{}{}
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
		deleteFunc: func(_ context.Context, id string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.deletedRow = append(h.deletedRow, id)
			return nil
		},
	}
	dc := &mockDockerClient{
		createContainerFunc: func(_ context.Context, opts docker.ContainerOptions) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.created = append(h.created, opts)
			return "docker-" + string(rune('0'+len(h.created))), nil
		},
		stopContainerFunc: func(_ context.Context, id string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.stopped = append(h.stopped, id)
			return nil
		},
		removeContainerFunc: func(_ context.Context, id string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.removed = append(h.removed, id)
			return nil
		},
	}
	h.svc = NewInstanceService(ir, cr, dc, catalog, "goforge_net", false, logger)

	inst := h.deploy(t)
	_, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{
		"name":            "vault",
		"domain":          "vault.example.com",
		"signups_allowed": "true",
		"admin_token":     "",
	})
	if err != nil {
		t.Fatalf("UpdateInstanceConfig: %v", err)
	}
	h.wait(t)

	var recreateLines []map[string]any
	for _, rec := range buf.records(t) {
		msg, _ := rec["msg"].(string)
		switch msg {
		case "service image present locally", "pulling service image", "pulled service image",
			"service container created and started", "service running":
			// Only count lines after edit: domain should be the new one when present.
			if domain, ok := rec["domain"].(string); ok && domain == "vault.example.com" {
				recreateLines = append(recreateLines, rec)
			}
		}
	}
	if len(recreateLines) == 0 {
		t.Fatalf("no recreate log lines with domain=vault.example.com; logs=%v", buf.records(t))
	}
	for _, rec := range recreateLines {
		if rec["domain"] != "vault.example.com" {
			t.Fatalf("domain=%v want vault.example.com in %#v", rec["domain"], rec)
		}
		if rec["template"] != "vaultwarden" {
			t.Fatalf("template=%v want vaultwarden in %#v", rec["template"], rec)
		}
		if rec["instance_id"] != inst.ID {
			t.Fatalf("instance_id=%v want %s", rec["instance_id"], inst.ID)
		}
	}
}
