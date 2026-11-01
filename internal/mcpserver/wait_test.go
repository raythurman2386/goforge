package mcpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// deployAPI stands in for /api/v1 during one deployment: trigger_deployment
// queues dep-1 and returns its operation; get_deployment reports it building
// for two reads, then failed with an error_message.
type deployAPI struct {
	mu    sync.Mutex
	reads int
}

func (d *deployAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/v1/projects/p1/deployments":
		_, _ = w.Write([]byte(`{"action":"trigger_deployment","dry_run":false,"approval_ref":"t1632u","audit_id":"a1",` +
			`"result":{"id":"dep-1","status":"pending"},` +
			`"operation":{"kind":"deployment","id":"dep-1","project_id":"p1","poll_tool":"wait_for"}}`))
	case "/api/v1/projects/p1/deployments/dep-1":
		d.mu.Lock()
		d.reads++
		n := d.reads
		d.mu.Unlock()
		if n <= 2 {
			_, _ = w.Write([]byte(`{"deployment":{"id":"dep-1","status":"building"},"terminal":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"deployment":{"id":"dep-1","status":"failed","error_message":"build failed: exit status 1"},"terminal":true}`))
	case "/api/v1/projects/p1/deployments/dep-1/logs":
		_, _ = w.Write([]byte(`{"lines":[{"level":"info","message":"step 3/7"},{"level":"error","message":"go: missing go.sum entry"}]}`))
	case "/api/v1/services/svc-slow":
		_, _ = w.Write([]byte(`{"service":{"id":"svc-slow","status":"pending"}}`))
	case "/api/v1/services/svc-bad":
		_, _ = w.Write([]byte(`{"service":{"id":"svc-bad","status":"failed","last_error":"image pull failed for x:1: manifest unknown"},"container":{"docker_id":"dock-9"}}`))
	case "/api/v1/containers/dock-9/logs":
		_, _ = w.Write([]byte(`{"lines":["boot","crash"]}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"not found"}}`))
	}
}

func shortPoll(t *testing.T) {
	t.Helper()
	prev := waitPollInterval
	waitPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { waitPollInterval = prev })
}

// trigger_deployment returns an operation; wait_for on it polls until the
// deployment fails and returns the error_message and the log tail.
func TestTriggerThenWaitForFailedDeployment(t *testing.T) {
	shortPoll(t)
	api := &deployAPI{}
	ts := httptest.NewServer(api)
	t.Cleanup(ts.Close)
	cs := connectTo(t, &Config{AllowWrites: true}, ts)

	res, text := callTool(t, cs, "trigger_deployment", map[string]any{"project_id": "p1", "dry_run": false, "approval_ref": "t1632u"})
	if res.IsError {
		t.Fatalf("trigger: %s", text)
	}
	op, _ := structured(t, res)["operation"].(map[string]any)
	if op["kind"] != "deployment" || op["id"] != "dep-1" || op["poll_tool"] != "wait_for" {
		t.Fatalf("operation = %v", op)
	}

	res, text = callTool(t, cs, "wait_for", map[string]any{"kind": op["kind"], "id": op["id"], "project_id": op["project_id"]})
	if res.IsError {
		t.Fatalf("wait_for: %s", text)
	}
	got := structured(t, res)
	if got["status"] != "failed" || got["terminal"] != true || got["timed_out"] != false ||
		got["error_message"] != "build failed: exit status 1" {
		t.Fatalf("wait_for = %v", got)
	}
	tail, _ := got["log_tail"].([]any)
	if got["log_source"] != "deployment_log" || len(tail) != 2 || !strings.Contains(tail[1].(string), "missing go.sum") {
		t.Errorf("log tail = %v from %v", tail, got["log_source"])
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.reads < 3 {
		t.Errorf("deployment read %d times, want polling until failed", api.reads)
	}
}

func TestWaitForServiceAndTimeout(t *testing.T) {
	shortPoll(t)
	ts := httptest.NewServer(&deployAPI{})
	t.Cleanup(ts.Close)
	cs := connectTo(t, &Config{}, ts)

	res, text := callTool(t, cs, "wait_for", map[string]any{"kind": "service", "id": "svc-bad"})
	got := structured(t, res)
	if res.IsError || got["status"] != "failed" || got["error_message"] != "image pull failed for x:1: manifest unknown" ||
		got["log_source"] != "container_log" {
		t.Fatalf("wait_for service = %s", text)
	}

	start := time.Now()
	res, text = callTool(t, cs, "wait_for", map[string]any{"kind": "service", "id": "svc-slow", "timeout_seconds": 1})
	got = structured(t, res)
	if res.IsError || got["timed_out"] != true || got["terminal"] != false || got["status"] != "pending" {
		t.Fatalf("slow service = %s", text)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("wait took %s, want about 1s", d)
	}

	res, text = callTool(t, cs, "wait_for", map[string]any{"kind": "deployment", "id": "dep-1"})
	wantError(t, res, text, http.StatusBadRequest, codeBadRequest)
	res, text = callTool(t, cs, "wait_for", map[string]any{"kind": "service", "id": "gone"})
	wantError(t, res, text, http.StatusNotFound, "not_found")
}
