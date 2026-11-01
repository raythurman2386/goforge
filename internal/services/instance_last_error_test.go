package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

const lastErrSecret = "s3cret-admin-token-t1632u"

// A failed image pull on deploy marks the instance failed and records why,
// with secret config values redacted.
func TestDeployInstance_PullFailureRecordsRedactedLastError(t *testing.T) {
	t.Parallel()
	catalog := NewCatalog("dummy")
	catalog.templates["vaultwarden"] = editTestTemplate()

	var mu sync.Mutex
	var final *models.ServiceInstance
	done := make(chan struct{}, 1)
	repo := &mockInstanceRepository{
		createFunc: func(_ context.Context, s *models.ServiceInstance) error {
			s.ID = "inst-pull-fail"
			return nil
		},
		getByIDFunc: func(_ context.Context, id string) (*models.ServiceInstance, error) {
			return &models.ServiceInstance{ID: id}, nil
		},
		updateFunc: func(_ context.Context, s *models.ServiceInstance) error {
			mu.Lock()
			cp := *s
			final = &cp
			mu.Unlock()
			done <- struct{}{}
			return nil
		},
	}
	pullErr := errors.New("pull access denied (token " + lastErrSecret + "): manifest unknown")
	dc := &mockDockerClient{
		imageExistsFunc: func(context.Context, string) (bool, error) { return false, nil },
		ensureImageFunc: func(context.Context, string) error { return pullErr },
		pullImageFunc:   func(context.Context, string) error { return pullErr },
	}
	svc := NewInstanceService(repo, &mockContainerRepository{}, dc, catalog, "goforge_net", false,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := svc.DeployInstance(context.Background(), "user-1", "vaultwarden", map[string]interface{}{
		"name": "vw", "domain": "vw.example.com", "signups_allowed": "true", "admin_token": lastErrSecret,
	}); err != nil {
		t.Fatalf("DeployInstance: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deploy goroutine did not record a status")
	}

	mu.Lock()
	defer mu.Unlock()
	if final.Status != models.ServiceStatusFailed {
		t.Fatalf("status = %s, want failed", final.Status)
	}
	got := final.LastError
	if !got.Valid || !strings.Contains(got.String, "image pull failed for vaultwarden/server:latest") ||
		!strings.Contains(got.String, "manifest unknown") {
		t.Errorf("last_error = %+v", got)
	}
	if strings.Contains(got.String, lastErrSecret) {
		t.Errorf("last_error leaks the secret: %q", got.String)
	}
}

// A failed pull during an image update leaves the old container running and
// the status as it was, but still records the failure. The next update that
// works clears it.
func TestUpdateInstanceImage_PullFailureRecordsLastErrorThenClears(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)

	h.mu.Lock()
	h.pullErr = errors.New("registry down")
	h.mu.Unlock()
	if _, err := h.svc.UpdateInstanceImage(context.Background(), inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	h.wait(t)

	h.mu.Lock()
	status, lastErr := h.instance.Status, h.instance.LastError
	h.pullErr = nil
	h.mu.Unlock()
	if status != models.ServiceStatusRunning {
		t.Errorf("status = %s, want running", status)
	}
	if !lastErr.Valid || !strings.Contains(lastErr.String, "registry down") || !strings.Contains(lastErr.String, "existing container left in place") {
		t.Errorf("last_error = %+v", lastErr)
	}

	if _, err := h.svc.UpdateInstanceImage(context.Background(), inst.ID); err != nil {
		t.Fatalf("second UpdateInstanceImage: %v", err)
	}
	h.wait(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.instance.Status != models.ServiceStatusRunning || h.instance.LastError.Valid {
		t.Errorf("after a good update: status=%s last_error=%+v, want running and cleared", h.instance.Status, h.instance.LastError)
	}
}

func TestLastErrorTextCapsLength(t *testing.T) {
	t.Parallel()
	got := lastErrorText("x", errors.New(strings.Repeat("é", 400)), nil)
	if len(got) > maxLastErrorLen || !strings.HasSuffix(got, "...") {
		t.Errorf("len %d, suffix %q", len(got), got[len(got)-3:])
	}
	if !strings.HasPrefix(got, "x: ") {
		t.Errorf("prefix %q", got[:5])
	}
}
