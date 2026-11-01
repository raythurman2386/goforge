package monitor

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/notify/notifytest"
)

// fakeDocker is a Docker inspector whose container states tests flip.
type fakeDocker struct {
	mu     sync.Mutex
	states map[string]*container.State
	calls  int
}

func (f *fakeDocker) set(id string, st *container.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.states == nil {
		f.states = map[string]*container.State{}
	}
	f.states[id] = st
}

func (f *fakeDocker) InspectContainer(_ context.Context, id string) (container.InspectResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	st, ok := f.states[id]
	if !ok {
		return container.InspectResponse{}, errors.New("no such container")
	}
	cp := *st
	return container.InspectResponse{State: &cp}, nil
}

func healthy() *container.State {
	return &container.State{Status: container.StateRunning, Running: true, Health: &container.Health{Status: container.Healthy}}
}

func unhealthy() *container.State {
	return &container.State{Status: container.StateRunning, Running: true, Health: &container.Health{
		Status: container.Unhealthy, FailingStreak: 3,
		Log: []*container.HealthcheckResult{{ExitCode: 1, Output: "curl: (7) connect refused\n"}},
	}}
}

func exited(code int) *container.State {
	return &container.State{Status: container.StateExited, ExitCode: code}
}

type fakeContainers struct{ rows []*models.Container }

func (f *fakeContainers) ListAll(context.Context) ([]*models.Container, error) { return f.rows, nil }

type fakeInstances map[string]*models.ServiceInstance

func (f fakeInstances) GetByID(_ context.Context, id string) (*models.ServiceInstance, error) {
	if i, ok := f[id]; ok {
		return i, nil
	}
	return nil, errors.New("not found")
}

type recNotifier struct {
	mu     sync.Mutex
	events []notify.Event
}

func (r *recNotifier) Notify(_ context.Context, ev notify.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recNotifier) all() []notify.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notify.Event(nil), r.events...)
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func row(dockerID, name string) *models.Container {
	return &models.Container{ID: "row-" + dockerID, DockerID: dockerID, Name: "/" + name, Status: models.ContainerStatusRunning, CreatedAt: t0.Add(-time.Hour)}
}

func newWatch(rows ...*models.Container) (*HealthWatch, *fakeDocker, *recNotifier, *clock) {
	d := &fakeDocker{}
	n := &recNotifier{}
	c := &clock{t: t0}
	w := &HealthWatch{Containers: &fakeContainers{rows: rows}, Instances: fakeInstances{}, Docker: d, Notifier: n, Now: c.now}
	return w, d, n, c
}

func TestHealthWatchUnhealthyAlertsExactlyOnce(t *testing.T) {
	w, d, n, c := newWatch(row("abc123", "shop-web"))
	ctx := context.Background()
	d.set("abc123", healthy())
	w.Tick(ctx)
	if got := n.all(); len(got) != 0 {
		t.Fatalf("healthy container alerted: %+v", got)
	}

	d.set("abc123", unhealthy())
	for range 10 {
		c.advance(DefaultHealthInterval)
		w.Tick(ctx)
	}
	got := n.all()
	if len(got) != 1 {
		t.Fatalf("got %d alerts over 10 unhealthy ticks, want exactly 1", len(got))
	}
	ev := got[0]
	if ev.Kind != notify.EventContainerUnhealthy || ev.Resource.Type != "container" || ev.Resource.ID != "abc123" || ev.Resource.Name != "shop-web" {
		t.Errorf("event %+v", ev)
	}
	if ev.Path != "/containers/abc123" || ev.Error != "health check failing (3 in a row): curl: (7) connect refused" {
		t.Errorf("path %q error %q", ev.Path, ev.Error)
	}
}

func TestHealthWatchDedupeAndCooldown(t *testing.T) {
	w, d, n, c := newWatch(row("abc123", "shop-web"))
	ctx := context.Background()

	d.set("abc123", unhealthy())
	w.Tick(ctx) // alert 1
	// Unhealthy to exited without recovering is the same episode: no alert.
	c.advance(time.Minute)
	d.set("abc123", exited(137))
	w.Tick(ctx)
	// Flapping inside the cooldown: recover, fail again, no new alert.
	c.advance(time.Minute)
	d.set("abc123", healthy())
	w.Tick(ctx)
	c.advance(time.Minute)
	d.set("abc123", unhealthy())
	w.Tick(ctx)
	if got := len(n.all()); got != 1 {
		t.Fatalf("got %d alerts during flapping inside the cooldown, want 1", got)
	}
	// Still failing when the cooldown ends is not a new episode either.
	c.advance(DefaultHealthCooldown)
	w.Tick(ctx)
	if got := len(n.all()); got != 1 {
		t.Fatalf("got %d alerts, want 1 (no repeat while it stays bad)", got)
	}
	// Recover, then fail after the cooldown: a new alert.
	c.advance(time.Minute)
	d.set("abc123", healthy())
	w.Tick(ctx)
	c.advance(time.Minute)
	d.set("abc123", exited(1))
	w.Tick(ctx)
	got := n.all()
	if len(got) != 2 {
		t.Fatalf("got %d alerts, want 2", len(got))
	}
	if got[1].Kind != notify.EventContainerExited || got[1].Error != "container exited (exit code 1)" {
		t.Errorf("second alert %+v", got[1])
	}
}

func TestHealthWatchSkips(t *testing.T) {
	young := row("young", "just-deployed")
	young.CreatedAt = t0.Add(-30 * time.Second)
	stoppedRow := row("stoppedrow", "stopped")
	stoppedRow.Status = models.ContainerStatusExited
	svcStopped := row("svcstopped", "pg")
	svcStopped.ServiceInstanceID = sql.NullString{String: "inst-stopped", Valid: true}
	svcRunning := row("svcrunning", "redis-1")
	svcRunning.ServiceInstanceID = sql.NullString{String: "inst-running", Valid: true}
	gone := row("gone", "gone")

	w, d, n, _ := newWatch(young, stoppedRow, svcStopped, svcRunning, gone)
	w.Instances = fakeInstances{
		"inst-stopped": {ID: "inst-stopped", Name: "pg", Status: models.ServiceStatusStopped},
		"inst-running": {ID: "inst-running", Name: "cache", Status: models.ServiceStatusRunning},
	}
	for _, id := range []string{"young", "stoppedrow", "svcstopped", "svcrunning"} {
		d.set(id, exited(1))
	}
	w.Tick(context.Background())
	got := n.all()
	if len(got) != 1 || got[0].Resource.ID != "svcrunning" || got[0].Resource.Name != "cache" {
		t.Fatalf("alerts %+v, want only the running service's container, named after the instance", got)
	}
}

func TestHealthWatchDisabledSkipsDocker(t *testing.T) {
	w, d, n, _ := newWatch(row("abc123", "shop-web"))
	w.Enabled = func(context.Context) bool { return false }
	d.set("abc123", unhealthy())
	w.Tick(context.Background())
	if len(n.all()) != 0 || d.calls != 0 {
		t.Fatalf("disabled watch inspected (%d) or alerted (%d)", d.calls, len(n.all()))
	}
}

func TestHealthWatchForgetsRemovedContainers(t *testing.T) {
	w, d, n, c := newWatch(row("abc123", "shop-web"))
	ctx := context.Background()
	d.set("abc123", unhealthy())
	w.Tick(ctx)
	w.Containers = &fakeContainers{}
	w.Tick(ctx)
	if len(w.state) != 0 {
		t.Fatalf("state kept for untracked container: %v", w.state)
	}
	// A redeploy reusing the row id but a new Docker id is a new container.
	w.Containers = &fakeContainers{rows: []*models.Container{row("def456", "shop-web")}}
	d.set("def456", unhealthy())
	c.advance(time.Minute)
	w.Tick(ctx)
	if got := len(n.all()); got != 2 {
		t.Fatalf("got %d alerts, want 2", got)
	}
}

func TestHealthWatchRestartingCountsAsExited(t *testing.T) {
	w, d, n, _ := newWatch(row("abc123", "shop-web"))
	d.set("abc123", &container.State{Status: container.StateRestarting, Restarting: true, ExitCode: 2})
	w.Tick(context.Background())
	if got := n.all(); len(got) != 1 || got[0].Kind != notify.EventContainerExited {
		t.Fatalf("alerts %+v", got)
	}
}

// End to end: the fake Docker flips to unhealthy and the httptest receiver
// gets exactly one POST through the real notifier.
func TestHealthWatchPostsOnceToReceiver(t *testing.T) {
	svc, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, "/hook-secret", "")
	w, d, _, c := newWatch(row("abc123", "shop-web"))
	w.Notifier = svc
	w.Enabled = svc.Enabled
	ctx := context.Background()
	d.set("abc123", healthy())
	w.Tick(ctx)
	d.set("abc123", unhealthy())
	for range 5 {
		c.advance(time.Minute)
		w.Tick(ctx)
	}
	svc.Wait()
	reqs := rcv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("receiver got %d POSTs, want 1", len(reqs))
	}
	p := reqs[0].Payload(t)
	if p.Event != notify.EventContainerUnhealthy || p.Link != "https://forge.example.test/containers/abc123" || p.Resource.ID != "abc123" {
		t.Errorf("payload %+v", p)
	}
}

func TestHealthWatchRunStopsOnCancel(t *testing.T) {
	w, d, n, _ := newWatch(row("abc123", "shop-web"))
	w.Interval = 5 * time.Millisecond
	d.set("abc123", exited(1))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for len(n.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	if len(n.all()) != 1 {
		t.Fatalf("got %d alerts", len(n.all()))
	}
}
