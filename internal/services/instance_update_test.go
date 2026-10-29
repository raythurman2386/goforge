package services

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestUpdateInstanceImage_PullsAndRecreatesWithSameVolumes(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceImage(context.Background(), inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()

	// The mock reports the image as present locally; latest is pulled anyway.
	wantPulls := []string{"vaultwarden/server:latest", "vaultwarden/server:latest"}
	if !slices.Equal(h.pulled, wantPulls) {
		t.Errorf("pulled = %v, want %v (deploy + update)", h.pulled, wantPulls)
	}
	// Pull before stopping, so a slow or failed pull does not take the service down.
	if want := []string{"pull", "stop", "remove", "create"}; !slices.Equal(h.events, want) {
		t.Errorf("update docker calls = %v, want %v", h.events, want)
	}

	if len(h.created) != 2 {
		t.Fatalf("created %d containers, want 2 (deploy + update)", len(h.created))
	}
	before, after := h.created[0], h.created[1]
	wantBinds := []string{
		"goforge-vol-" + inst.ID + "-data:/data",
		"goforge-vol-" + inst.ID + "-attachments:/data/attachments",
	}
	if !slices.Equal(after.Binds, wantBinds) || !slices.Equal(after.Binds, before.Binds) {
		t.Errorf("update binds = %v, want deploy volumes %v", after.Binds, wantBinds)
	}
	if after.Name != before.Name || after.Image != before.Image {
		t.Errorf("container changed: name %q -> %q, image %q -> %q", before.Name, after.Name, before.Image, after.Image)
	}
	if after.NanoCPUs != before.NanoCPUs || after.Memory != before.Memory {
		t.Errorf("limits changed: (%d, %d) -> (%d, %d)", before.NanoCPUs, before.Memory, after.NanoCPUs, after.Memory)
	}
	if !maps.Equal(envMap(before), envMap(after)) {
		t.Errorf("env changed on update:\nbefore %v\nafter  %v", envMap(before), envMap(after))
	}
	if !slices.Equal(h.stopped, []string{"docker-1"}) || !slices.Equal(h.removed, []string{"docker-1"}) {
		t.Errorf("stopped=%v removed=%v, want docker-1", h.stopped, h.removed)
	}
	if h.container == nil || h.container.DockerID != "docker-2" {
		t.Errorf("container record = %+v, want docker-2", h.container)
	}
	if h.instance.Status != models.ServiceStatusRunning || h.instance.ID != inst.ID {
		t.Errorf("instance = %s/%s, want running with same ID %s", h.instance.ID, h.instance.Status, inst.ID)
	}
}

func TestUpdateInstanceImage_PullFailureLeavesContainerRunning(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	h.mu.Lock()
	h.pullErr = errors.New("registry down")
	h.events = nil
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceImage(context.Background(), inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	// Update must not fall back to the local image: that would recreate on the old one.
	if want := []string{"pull"}; !slices.Equal(h.events, want) {
		t.Errorf("docker calls after failed pull = %v, want %v", h.events, want)
	}
	if len(h.created) != 1 || len(h.stopped) != 0 || len(h.removed) != 0 {
		t.Errorf("container touched: created=%d stopped=%v removed=%v", len(h.created), h.stopped, h.removed)
	}
	if h.instance.Status != models.ServiceStatusRunning {
		t.Errorf("status = %s, want running (previous status restored)", h.instance.Status)
	}
}

func TestUpdateInstanceImage_RejectsWhileDeploying(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	h.instance = &models.ServiceInstance{
		ID:           "inst-busy",
		TemplateName: "vaultwarden",
		Name:         "vw",
		Status:       models.ServiceStatusStarting,
		Version:      sql.NullString{String: "latest", Valid: true},
	}

	if _, err := h.svc.UpdateInstanceImage(context.Background(), "inst-busy"); !errors.Is(err, ErrInstanceBusy) {
		t.Fatalf("err = %v, want ErrInstanceBusy", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) != 0 {
		t.Errorf("docker touched while busy: %v", h.events)
	}
}

// A config edit still falls back to the local image when the registry is down.
func TestUpdateInstanceConfig_PullFailureUsesLocalImage(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	h.mu.Lock()
	h.pullErr = errors.New("registry down")
	h.mu.Unlock()

	if _, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{"signups_allowed": "false"}); err != nil {
		t.Fatalf("UpdateInstanceConfig: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 2 || h.instance.Status != models.ServiceStatusRunning {
		t.Fatalf("created=%d status=%s, want recreate on the local image", len(h.created), h.instance.Status)
	}
	if !slices.Equal(h.created[1].Binds, h.created[0].Binds) {
		t.Errorf("binds changed: %v -> %v", h.created[0].Binds, h.created[1].Binds)
	}
}
