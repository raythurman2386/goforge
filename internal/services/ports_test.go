package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

var giteaSSHBinding = docker.PortBinding{ContainerPort: "22", Protocol: "tcp", HostIP: "127.0.0.1", HostPort: "2223"}

func repoSpec(t *testing.T, c *Catalog, name string, config map[string]interface{}) *containerSpec {
	t.Helper()
	tmpl, err := c.GetTemplate(name)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewInstanceService(nil, nil, nil, c, "traefik", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	inst := &models.ServiceInstance{
		ID:      "inst-" + name,
		Version: sql.NullString{String: tmpl.DefaultVersion, Valid: true},
		Domain:  sql.NullString{String: name + "-app.example.com", Valid: true},
	}
	spec, err := svc.buildContainerSpec(tmpl, inst, config)
	if err != nil {
		t.Fatalf("%s: buildContainerSpec: %v", name, err)
	}
	return spec
}

// Only Gitea publishes a port, and only SSH on loopback 2223 by default.
func TestBuildContainerSpec_PublishedPorts(t *testing.T) {
	t.Parallel()
	c := loadRepoCatalog(t)
	for _, tmpl := range c.ListTemplates() {
		t.Run(tmpl.Name, func(t *testing.T) {
			t.Parallel()
			spec := repoSpec(t, c, tmpl.Name, goldenConfig(tmpl, ""))
			var want []docker.PortBinding
			if tmpl.Name == "gitea" {
				want = []docker.PortBinding{giteaSSHBinding}
			}
			if !slices.Equal(spec.opts.PortMappings, want) {
				t.Errorf("PortMappings = %+v, want %+v", spec.opts.PortMappings, want)
			}
		})
	}
}

// An instance deployed before ssh_port and ssh_domain existed (the live Gitea)
// has neither in its stored config. It must render the defaults, never
// "<no value>" and never an empty port.
func TestBuildContainerSpec_GiteaStoredConfigWithoutSSHFields(t *testing.T) {
	t.Parallel()
	c := loadRepoCatalog(t)
	stored := map[string]interface{}{
		"name": "gitea", "domain": "gitea-app.example.com", "app_name": "Gitea",
		"disable_registration": "true", "require_signin_view": "true",
	}
	spec := repoSpec(t, c, "gitea", stored)

	if !slices.Equal(spec.opts.PortMappings, []docker.PortBinding{giteaSSHBinding}) {
		t.Errorf("PortMappings = %+v, want [%+v]", spec.opts.PortMappings, giteaSSHBinding)
	}
	env := envMap(spec.opts)
	if got := env["GITEA__SERVER__SSH_PORT"]; got != "2223" {
		t.Errorf("SSH_PORT = %q, want 2223", got)
	}
	if got := env["GITEA__SERVER__SSH_DOMAIN"]; got != "minis" {
		t.Errorf("SSH_DOMAIN = %q, want minis", got)
	}
	for _, kv := range spec.opts.Env {
		if strings.Contains(kv, "<no value>") {
			t.Errorf("env renders <no value>: %s", kv)
		}
	}
	for _, h := range spec.hooks {
		for _, kv := range append(slices.Clone(h.exec.Cmd), h.exec.Env...) {
			if strings.Contains(kv, "<no value>") {
				t.Errorf("hook %s renders <no value>: %s", h.name, kv)
			}
		}
	}
	if _, ok := stored["ssh_port"]; ok {
		t.Error("buildContainerSpec wrote ssh_port into the stored config")
	}
}

func TestBuildContainerSpec_GiteaSSHFields(t *testing.T) {
	t.Parallel()
	c := loadRepoCatalog(t)
	tests := []struct {
		name       string
		port       interface{}
		domain     interface{}
		wantPort   string
		wantDomain string // "" means SSH_DOMAIN is left out
		wantErr    bool
	}{
		{name: "custom port and domain", port: "2299", domain: "git.tailnet", wantPort: "2299", wantDomain: "git.tailnet"},
		{name: "number from yaml default", port: 2223, domain: "minis", wantPort: "2223", wantDomain: "minis"},
		{name: "blank domain falls back to DOMAIN", port: "2223", domain: "", wantPort: "2223"},
		{name: "blank port", port: "", domain: "minis", wantErr: true},
		{name: "not a number", port: "ssh", domain: "minis", wantErr: true},
		{name: "zero", port: "0", domain: "minis", wantErr: true},
		{name: "too big", port: "70000", domain: "minis", wantErr: true},
	}
	tmpl, _ := c.GetTemplate("gitea")
	svc := NewInstanceService(nil, nil, nil, c, "traefik", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := goldenConfig(tmpl, "")
			config["ssh_port"], config["ssh_domain"] = tt.port, tt.domain
			inst := &models.ServiceInstance{ID: "x", Version: sql.NullString{String: "1.25.5", Valid: true}}
			spec, err := svc.buildContainerSpec(tmpl, inst, config)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("err = %v, want ErrInvalidConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []docker.PortBinding{{ContainerPort: "22", Protocol: "tcp", HostIP: "127.0.0.1", HostPort: tt.wantPort}}
			if !slices.Equal(spec.opts.PortMappings, want) {
				t.Errorf("PortMappings = %+v, want %+v", spec.opts.PortMappings, want)
			}
			env := envMap(spec.opts)
			if env["GITEA__SERVER__SSH_PORT"] != tt.wantPort {
				t.Errorf("SSH_PORT = %q, want %q", env["GITEA__SERVER__SSH_PORT"], tt.wantPort)
			}
			gotDomain, set := env["GITEA__SERVER__SSH_DOMAIN"]
			if tt.wantDomain == "" && set {
				t.Errorf("SSH_DOMAIN = %q, want unset", gotDomain)
			} else if tt.wantDomain != "" && gotDomain != tt.wantDomain {
				t.Errorf("SSH_DOMAIN = %q, want %q", gotDomain, tt.wantDomain)
			}
		})
	}
}

func TestCatalog_PortValidation(t *testing.T) {
	t.Parallel()
	const head = "name: t\ndefault_version: \"1\"\ndocker:\n  image: x:{{ .Version }}\n  ports:\n    - container: 80\n      protocol: tcp\n"
	tests := []struct {
		name    string
		port    string
		wantErr string
		wantIP  string
	}{
		{name: "publish without host_port", port: "    - container: 22\n      publish: true\n", wantErr: "publish needs host_port"},
		{name: "0.0.0.0 refused", port: "    - container: 22\n      publish: true\n      host_port: \"2223\"\n      host_ip: 0.0.0.0\n", wantErr: "refused"},
		{name: ":: refused", port: "    - container: 22\n      publish: true\n      host_port: \"2223\"\n      host_ip: \"::\"\n", wantErr: "refused"},
		{name: "LAN address refused", port: "    - container: 22\n      publish: true\n      host_port: \"2223\"\n      host_ip: 192.168.1.10\n", wantErr: "loopback only"},
		{name: "not an IP", port: "    - container: 22\n      publish: true\n      host_port: \"2223\"\n      host_ip: minis\n", wantErr: "not an IP"},
		{name: "host_port without publish", port: "    - container: 22\n      host_port: \"2223\"\n", wantErr: "need publish: true"},
		{name: "bad template", port: "    - container: 22\n      publish: true\n      host_port: \"{{ .Config.ssh_port \"\n", wantErr: "host_port"},
		{name: "bad protocol", port: "    - container: 22\n      protocol: sctp\n", wantErr: "tcp or udp"},
		{name: "empty host_ip defaults to loopback", port: "    - container: 22\n      publish: true\n      host_port: \"2223\"\n", wantIP: "127.0.0.1"},
		{name: "ipv6 loopback allowed", port: "    - container: 22\n      publish: true\n      host_port: \"2223\"\n      host_ip: \"::1\"\n", wantIP: "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte(head+tt.port), 0o600); err != nil {
				t.Fatal(err)
			}
			c := NewCatalog(dir)
			err := c.LoadTemplates()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadTemplates err = %v, want %q", err, tt.wantErr)
				}
				if c.Count() != 0 {
					t.Error("invalid template was loaded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tmpl, _ := c.GetTemplate("t")
			if got := tmpl.Docker.Ports[1].HostIP; got != tt.wantIP {
				t.Errorf("HostIP = %q, want %q", got, tt.wantIP)
			}
			if got := tmpl.Docker.Ports[0]; got.Publish || got.HostIP != "" {
				t.Errorf("unpublished port changed: %+v", got)
			}
		})
	}
}

// publishTemplate is the edit-harness template with SSH published on a
// configurable loopback port.
func publishTemplate() *ServiceTemplate {
	tmpl := editTestTemplate()
	tmpl.Config = append(tmpl.Config, ConfigField{Name: "ssh_port", Type: FieldTypeNumber, Default: 2223, Required: true})
	tmpl.Docker.Ports = append(tmpl.Docker.Ports, PortConfig{Container: 22, Protocol: "tcp", Publish: true, HostPort: "{{ .Config.ssh_port }}", HostIP: "127.0.0.1"})
	return tmpl
}

func newPublishHarness(t *testing.T) (*editHarness, *mockDockerClient) {
	t.Helper()
	h := newEditHarness(t)
	h.svc.catalog.templates["vaultwarden"] = publishTemplate()
	dc := h.svc.dockerClient.(*mockDockerClient)
	dc.renameContainerFunc = func(_ context.Context, id, name string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.events = append(h.events, "rename "+id+" "+name)
		return nil
	}
	dc.startContainerFunc = func(_ context.Context, id string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.events = append(h.events, "start "+id)
		return nil
	}
	return h, dc
}

func TestDeployPublished_RecordsPorts(t *testing.T) {
	t.Parallel()
	h, _ := newPublishHarness(t)
	h.deploy(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	if got := h.created[0].PortMappings; !slices.Equal(got, []docker.PortBinding{giteaSSHBinding}) {
		t.Errorf("PortMappings = %+v", got)
	}
	var ports []models.PortMapping
	if err := json.Unmarshal(h.container.Ports, &ports); err != nil {
		t.Fatal(err)
	}
	want := []models.PortMapping{{Host: 2223, Container: 22, Protocol: "tcp", HostIP: "127.0.0.1"}}
	if !slices.Equal(ports, want) {
		t.Errorf("containers.ports = %+v, want %+v", ports, want)
	}
	rows := PublishedPortRows(h.container)
	if len(rows) != 1 || rows[0].Key != "published 22/tcp" || rows[0].Display != "127.0.0.1:2223" || rows[0].Secret {
		t.Errorf("PublishedPortRows = %+v", rows)
	}
}

func TestDeployUnpublished_RecordsNoPorts(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	h.deploy(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.created[0].PortMappings != nil {
		t.Errorf("PortMappings = %+v, want nil", h.created[0].PortMappings)
	}
	if string(h.container.Ports) != "[]" {
		t.Errorf("containers.ports = %s, want []", h.container.Ports)
	}
	if rows := PublishedPortRows(h.container); len(rows) != 0 {
		t.Errorf("PublishedPortRows = %+v, want none", rows)
	}
}

func TestDeployPublished_BadPortRejectedBeforeRecord(t *testing.T) {
	t.Parallel()
	h, _ := newPublishHarness(t)
	_, err := h.svc.DeployInstance(context.Background(), "user-1", "vaultwarden", map[string]interface{}{
		"name": "vw", "ssh_port": "nope",
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.instance != nil || len(h.created) != 0 {
		t.Errorf("deploy went ahead: instance=%v created=%d", h.instance, len(h.created))
	}
}

func TestUpdateInstanceConfig_BadPublishedPortLeavesContainer(t *testing.T) {
	t.Parallel()
	h, _ := newPublishHarness(t)
	inst := h.deploy(t)
	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()

	_, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{"ssh_port": "99999"})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) != 0 {
		t.Errorf("docker touched by a rejected edit: %v", h.events)
	}
	if h.instance.Status != models.ServiceStatusRunning {
		t.Errorf("status = %s, want running", h.instance.Status)
	}
}

// A recreate that publishes a port parks the old container and removes it
// only after the new one starts.
func TestUpdateInstanceConfig_PublishedParksOldUntilNewStarts(t *testing.T) {
	t.Parallel()
	h, _ := newPublishHarness(t)
	inst := h.deploy(t)
	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{"ssh_port": "2299"}); err != nil {
		t.Fatal(err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) != 5 || h.events[0] != "pull" || h.events[1] != "stop" || !strings.HasPrefix(h.events[2], "rename docker-1 goforge-service-"+inst.ID+"-replaced-") ||
		h.events[3] != "create" || h.events[4] != "remove" {
		t.Errorf("docker calls = %v, want pull, stop, rename aside, create, remove", h.events)
	}
	if !slices.Equal(h.removed, []string{"docker-1"}) {
		t.Errorf("removed = %v, want docker-1", h.removed)
	}
	if got := h.created[1].PortMappings[0].HostPort; got != "2299" {
		t.Errorf("new host port = %s, want 2299", got)
	}
	if h.instance.Status != models.ServiceStatusRunning || h.container.DockerID != "docker-2" {
		t.Errorf("instance %s container %s, want running docker-2", h.instance.Status, h.container.DockerID)
	}
	if !slices.Equal(h.deletedRow, []string{"row-docker-1"}) {
		t.Errorf("deleted rows = %v, want row-docker-1", h.deletedRow)
	}
}

// When the new container cannot bind its host port, the old container gets
// its name back and runs again; nothing is removed.
func TestUpdateInstanceImage_PortTakenRestoresOldContainer(t *testing.T) {
	t.Parallel()
	h, dc := newPublishHarness(t)
	inst := h.deploy(t)

	dc.createContainerFunc = func(_ context.Context, opts docker.ContainerOptions) (string, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.events = append(h.events, "create")
		// Docker creates the container, then start fails on the bind.
		return "half-made", errors.New("failed to start container: Bind for 127.0.0.1:2223 failed: port is already allocated")
	}
	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceImage(context.Background(), inst.ID); err != nil {
		t.Fatal(err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	name := "goforge-service-" + inst.ID
	want := []string{"pull", "stop", "", "create", "remove", "rename docker-1 " + name, "start docker-1"}
	if len(h.events) != len(want) {
		t.Fatalf("docker calls = %v, want %v", h.events, want)
	}
	for i, w := range want {
		if w == "" {
			if !strings.HasPrefix(h.events[i], "rename docker-1 "+name+"-replaced-") {
				t.Errorf("call %d = %q, want rename aside", i, h.events[i])
			}
			continue
		}
		if h.events[i] != w {
			t.Errorf("call %d = %q, want %q", i, h.events[i], w)
		}
	}
	if !slices.Equal(h.removed, []string{"half-made"}) {
		t.Errorf("removed = %v, want only the container that never started", h.removed)
	}
	if len(h.deletedRow) != 0 || h.container.DockerID != "docker-1" {
		t.Errorf("container row changed: deleted=%v current=%s", h.deletedRow, h.container.DockerID)
	}
	if h.instance.Status != models.ServiceStatusFailed {
		t.Errorf("status = %s, want failed", h.instance.Status)
	}
}

// If the old container cannot be renamed aside, the recreate falls back to
// the usual stop, remove, create.
func TestUpdateInstanceConfig_PublishedRenameFailsFallsBack(t *testing.T) {
	t.Parallel()
	h, dc := newPublishHarness(t)
	inst := h.deploy(t)
	dc.renameContainerFunc = func(context.Context, string, string) error { return errors.New("no such container") }
	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{"ssh_port": "2299"}); err != nil {
		t.Fatal(err)
	}
	h.wait(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	if want := []string{"pull", "stop", "remove", "create"}; !slices.Equal(h.events, want) {
		t.Errorf("docker calls = %v, want %v", h.events, want)
	}
	if h.instance.Status != models.ServiceStatusRunning {
		t.Errorf("status = %s, want running", h.instance.Status)
	}
}

func TestTemplateConfig_FillsDefaults(t *testing.T) {
	t.Parallel()
	tmpl := &ServiceTemplate{Config: []ConfigField{
		{Name: "port", Type: FieldTypeNumber, Default: 2223},
		{Name: "host", Default: "minis"},
		{Name: "secret", Type: "password", Generate: true},
		{Name: "set", Default: "x"},
	}}
	stored := map[string]interface{}{"set": "kept", "extra": "stays"}
	got := templateConfig(tmpl, stored)
	want := map[string]interface{}{"port": 2223, "host": "minis", "secret": "", "set": "kept", "extra": "stays"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v, want %#v", k, got[k], v)
		}
	}
	if len(stored) != 2 {
		t.Errorf("stored config modified: %v", stored)
	}
}
