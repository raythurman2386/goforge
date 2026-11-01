package notify

import (
	"context"
	"time"
)

// Event kinds. The webhook payload's "event" field is one of these.
const (
	EventDeployFailed       = "deploy.failed"
	EventServiceFailed      = "service.failed"
	EventBackupFailed       = "backup.failed"
	EventContainerUnhealthy = "container.unhealthy"
	EventContainerExited    = "container.exited"
	EventTest               = "test"
)

// Resource is what the event is about.
type Resource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Event is one alert. Error is redacted before it is sent. Path is a
// GoForge path such as /services/<id>; the notifier joins it to
// GOFORGE_BASE_URL to make the link.
type Event struct {
	Kind     string
	Title    string
	Resource Resource
	Error    string
	Path     string
	AuditID  string
	Time     time.Time
}

// Notifier sends events. Notify must not block the caller for long and
// never returns an error: a failed alert is logged, not retried.
type Notifier interface {
	Notify(ctx context.Context, ev Event)
}

// titles are the default titles per kind.
var titles = map[string]string{
	EventDeployFailed:       "Deployment failed",
	EventServiceFailed:      "Service failed",
	EventBackupFailed:       "Backup failed",
	EventContainerUnhealthy: "Container unhealthy",
	EventContainerExited:    "Container exited",
	EventTest:               "Test notification",
}

func (ev Event) title() string {
	t := ev.Title
	if t == "" {
		t = titles[ev.Kind]
	}
	if t == "" {
		t = ev.Kind
	}
	if ev.Resource.Name != "" {
		t += ": " + ev.Resource.Name
	}
	return "GoForge: " + t
}
