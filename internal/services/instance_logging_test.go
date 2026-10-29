package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) contains(sub string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.buf.String(), sub)
}

// records returns each JSON log line as a map.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(b.buf.String()), "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func messages(recs []map[string]any, level string) []string {
	var out []string
	for _, r := range recs {
		if r["level"] == level {
			out = append(out, r["msg"].(string))
		}
	}
	return out
}

type logDeployResult struct {
	recs    []map[string]any
	status  models.ServiceInstanceStatus
	created int
	pulled  int
}

func runLoggedDeploy(t *testing.T, imageExists bool, pullErr error) logDeployResult {
	t.Helper()
	catalog := NewCatalog("dummy")
	catalog.templates["web"] = &ServiceTemplate{
		Name:           "web",
		DefaultVersion: "1.0",
		Docker:         DockerConfig{Image: "example/web:{{ .Version }}"},
	}

	var mu sync.Mutex
	var res logDeployResult
	done := make(chan struct{})
	var once sync.Once
	finish := func(status models.ServiceInstanceStatus) {
		mu.Lock()
		res.status = status
		mu.Unlock()
		once.Do(func() { close(done) })
	}

	repo := &mockInstanceRepository{
		createFunc: func(_ context.Context, s *models.ServiceInstance) error {
			s.ID = "inst-log"
			return nil
		},
		getByIDFunc: func(_ context.Context, id string) (*models.ServiceInstance, error) {
			return &models.ServiceInstance{ID: id}, nil
		},
		updateFunc: func(_ context.Context, s *models.ServiceInstance) error {
			finish(s.Status)
			return nil
		},
	}
	dc := &mockDockerClient{
		imageExistsFunc: func(_ context.Context, _ string) (bool, error) { return imageExists, nil },
		ensureImageFunc: func(_ context.Context, _ string) error {
			mu.Lock()
			res.pulled++
			mu.Unlock()
			return pullErr
		},
		createContainerFunc: func(_ context.Context, _ docker.ContainerOptions) (string, error) {
			mu.Lock()
			res.created++
			mu.Unlock()
			return "docker-log", nil
		},
	}

	buf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	svc := NewInstanceService(repo, &mockContainerRepository{}, dc, catalog, "goforge_net", false, logger)

	if _, err := svc.DeployInstance(context.Background(), "user1", "web", map[string]interface{}{
		"name": "site", "domain": "site.example.com",
	}); err != nil {
		t.Fatalf("DeployInstance: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for deploy goroutine")
	}
	// On success the final Info line is written after Update returns.
	mu.Lock()
	running := res.status == models.ServiceStatusRunning
	mu.Unlock()
	if running {
		deadline := time.Now().Add(5 * time.Second)
		for !buf.contains(`"msg":"service running"`) {
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for service running log")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	res.recs = buf.records(t)
	return res
}

func TestDeployInstance_LogsHappyPathWithPull(t *testing.T) {
	t.Parallel()
	res := runLoggedDeploy(t, false, nil)

	want := []string{
		"service deploy requested",
		"pulling service image",
		"pulled service image",
		"service container created and started",
		"service running",
	}
	got := messages(res.recs, "INFO")
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("info logs = %q, want %q", got, want)
	}
	for _, r := range res.recs {
		if r["instance_id"] != "inst-log" || r["template"] != "web" || r["image"] != "example/web:1.0" || r["domain"] != "site.example.com" {
			t.Errorf("log %q missing deploy attrs: %v", r["msg"], r)
		}
	}
	for _, r := range res.recs {
		switch r["msg"] {
		case "pulled service image":
			if r["duration"] == nil {
				t.Error("pulled log has no duration")
			}
		case "service container created and started":
			if r["docker_id"] != "docker-log" || r["container"] != "goforge-service-inst-log" {
				t.Errorf("created log attrs = %v", r)
			}
		}
	}
	if res.pulled != 1 || res.status != models.ServiceStatusRunning {
		t.Errorf("pulled=%d status=%s", res.pulled, res.status)
	}
}

func TestDeployInstance_LogsSkipPullWhenImagePresent(t *testing.T) {
	t.Parallel()
	res := runLoggedDeploy(t, true, nil)

	want := []string{
		"service deploy requested",
		"service image present locally",
		"service container created and started",
		"service running",
	}
	got := messages(res.recs, "INFO")
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("info logs = %q, want %q", got, want)
	}
	if res.pulled != 0 {
		t.Errorf("EnsureImage called %d times for a local image", res.pulled)
	}
}

func TestDeployInstance_PullFailureLogsErrorAndFails(t *testing.T) {
	t.Parallel()
	res := runLoggedDeploy(t, false, errors.New("registry down"))

	if res.status != models.ServiceStatusFailed {
		t.Errorf("status = %s, want failed", res.status)
	}
	if res.created != 0 {
		t.Errorf("container created after failed pull")
	}
	if errs := messages(res.recs, "ERROR"); len(errs) != 1 || errs[0] != "failed to pull service image" {
		t.Errorf("error logs = %q", errs)
	}
	for _, m := range messages(res.recs, "INFO") {
		if m == "service running" || m == "pulled service image" {
			t.Errorf("unexpected success log %q", m)
		}
	}
}
