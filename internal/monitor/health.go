package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
)

// Health watch defaults.
const (
	// DefaultHealthInterval is how often the watch inspects tracked containers.
	DefaultHealthInterval = time.Minute
	// DefaultHealthGrace skips containers created this recently, so a
	// deploy's own health check and blue-green swap are not reported twice
	// (a failed deploy already sends deploy.failed).
	DefaultHealthGrace = 2 * time.Minute
	// DefaultHealthCooldown is the least time between two alerts for the
	// same container, so a restart loop does not page every minute.
	DefaultHealthCooldown = 30 * time.Minute
)

// HealthContainers lists the containers GoForge tracks
// (repositories.ContainerRepository).
type HealthContainers interface {
	ListAll(ctx context.Context) ([]*models.Container, error)
}

// HealthInstances reads a service instance
// (repositories.ServiceInstanceRepository).
type HealthInstances interface {
	GetByID(ctx context.Context, id string) (*models.ServiceInstance, error)
}

// HealthInspector inspects a Docker container (*docker.Client).
type HealthInspector interface {
	InspectContainer(ctx context.Context, id string) (container.InspectResponse, error)
}

// healthState is what the watch remembers about one container.
type healthState struct {
	problem   string // "" when last seen fine
	lastAlert time.Time
}

// HealthWatch alerts when a tracked container that should be running goes
// unhealthy or exits. A container is watched when its containers row says
// running, it is older than Grace, and (for a service container) its
// instance is running. Alerts are deduplicated: one per episode (no repeat
// while the container stays bad, even if the problem changes), and at most
// one per container per Cooldown. A container that recovers and fails again
// after the cooldown alerts again.
type HealthWatch struct {
	Containers HealthContainers
	Instances  HealthInstances
	Docker     HealthInspector
	Notifier   notify.Notifier
	// Enabled, when set, skips a tick (no Docker calls) while it is false,
	// such as when notifications are off.
	Enabled  func(ctx context.Context) bool
	Interval time.Duration
	Grace    time.Duration
	Cooldown time.Duration
	Logger   *slog.Logger
	Now      func() time.Time

	mu    sync.Mutex
	state map[string]*healthState
}

func (w *HealthWatch) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *HealthWatch) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// Run ticks until ctx is done.
func (w *HealthWatch) Run(ctx context.Context) {
	t := time.NewTicker(orDefault(w.Interval, DefaultHealthInterval))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Tick(ctx)
		}
	}
}

// problemOf describes what is wrong with an inspected container, or "".
func problemOf(info container.InspectResponse) (kind, problem string) {
	st := info.State
	if st == nil {
		return "", ""
	}
	switch st.Status {
	case container.StateExited, container.StateDead:
		p := fmt.Sprintf("container %s (exit code %d)", st.Status, st.ExitCode)
		if st.OOMKilled {
			p += ", killed for running out of memory"
		}
		if st.Error != "" {
			p += ": " + st.Error
		}
		return notify.EventContainerExited, p
	case container.StateRestarting:
		return notify.EventContainerExited, fmt.Sprintf("container restarting (last exit code %d, %d restarts)", st.ExitCode, info.RestartCount)
	case container.StateCreated, container.StateRunning, container.StatePaused, container.StateRemoving:
		// Fine as far as the state goes; the health check decides.
	}
	if st.Health != nil && st.Health.Status == container.Unhealthy {
		p := fmt.Sprintf("health check failing (%d in a row)", st.Health.FailingStreak)
		if n := len(st.Health.Log); n > 0 {
			if out := strings.TrimSpace(st.Health.Log[n-1].Output); out != "" {
				p += ": " + out
			}
		}
		return notify.EventContainerUnhealthy, p
	}
	return "", ""
}

// Tick inspects every watched container once and sends alerts.
func (w *HealthWatch) Tick(ctx context.Context) {
	if w.Enabled != nil && !w.Enabled(ctx) {
		return
	}
	rows, err := w.Containers.ListAll(ctx)
	if err != nil {
		w.logger().Warn("health watch: list containers failed", "error", err)
		return
	}
	now := w.now()
	grace := orDefault(w.Grace, DefaultHealthGrace)
	cooldown := orDefault(w.Cooldown, DefaultHealthCooldown)
	seen := make(map[string]bool, len(rows))
	for _, c := range rows {
		if c.Status != models.ContainerStatusRunning || now.Sub(c.CreatedAt) < grace {
			continue
		}
		res, ok := w.resource(ctx, c)
		if !ok {
			continue
		}
		seen[c.DockerID] = true
		info, ierr := w.Docker.InspectContainer(ctx, c.DockerID)
		if ierr != nil {
			// Gone or Docker down: not an alert from this watch.
			continue
		}
		kind, problem := problemOf(info)
		if w.record(c.DockerID, problem, now, cooldown) {
			w.Notifier.Notify(ctx, notify.Event{
				Kind:     kind,
				Resource: res,
				Error:    problem,
				Path:     "/containers/" + c.DockerID,
				Time:     now,
			})
			w.logger().Warn("health watch: container alert", "docker_id", c.DockerID, "event", kind)
		}
	}
	w.forget(seen)
}

// resource names the container for the alert. It reports false for a
// service container whose instance is not running (stopped by the user,
// failed and already reported, or mid-change).
func (w *HealthWatch) resource(ctx context.Context, c *models.Container) (notify.Resource, bool) {
	res := notify.Resource{Type: models.ResourceTypeContainer, ID: c.DockerID, Name: strings.TrimPrefix(c.Name, "/")}
	if c.ServiceInstanceID.Valid && w.Instances != nil {
		inst, err := w.Instances.GetByID(ctx, c.ServiceInstanceID.String)
		if err != nil || inst.Status != models.ServiceStatusRunning {
			return res, false
		}
		if inst.Name != "" {
			res.Name = inst.Name
		}
	}
	return res, true
}

// record stores the container's current problem and reports whether to
// alert: the problem is new, and the container has not alerted within
// cooldown.
func (w *HealthWatch) record(id, problem string, now time.Time, cooldown time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == nil {
		w.state = map[string]*healthState{}
	}
	st := w.state[id]
	if st == nil {
		st = &healthState{}
		w.state[id] = st
	}
	prevBad := st.problem != ""
	st.problem = problem
	if problem == "" || prevBad {
		return false
	}
	if !st.lastAlert.IsZero() && now.Sub(st.lastAlert) < cooldown {
		return false
	}
	st.lastAlert = now
	return true
}

// forget drops state for containers no longer watched.
func (w *HealthWatch) forget(seen map[string]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id := range w.state {
		if !seen[id] {
			delete(w.state, id)
		}
	}
}
