package pages_services //nolint:revive,stylecheck // matches the templ package name

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
)

func renderDetail(t *testing.T, status models.ServiceInstanceStatus, c *models.Container, s *docker.InspectSummary) string {
	t.Helper()
	inst := &models.ServiceInstance{ID: "inst-1", Name: "verify-pg", Status: status}
	var buf bytes.Buffer
	if err := Detail(inst, c, s, &services.ServiceTemplate{DisplayName: "PostgreSQL"}, "csrf").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

var divTag = regexp.MustCompile(`<div\b|</div>`)

// innerRange returns the byte range of the element whose opening tag holds
// marker, matching <div> nesting to find its close.
func innerRange(t *testing.T, html, marker string) (int, int) {
	t.Helper()
	at := strings.Index(html, marker)
	if at < 0 {
		t.Fatalf("marker %q not found", marker)
	}
	open := strings.LastIndex(html[:at], "<div")
	depth := 0
	for _, loc := range divTag.FindAllStringIndex(html[open:], -1) {
		if html[open+loc[0]:open+loc[1]] == "</div>" {
			depth--
		} else {
			depth++
		}
		if depth == 0 {
			return open, open + loc[1]
		}
	}
	t.Fatalf("element with %q is not closed", marker)
	return 0, 0
}

// The edit-save bug: the pending poll sat outside #service-detail-content,
// so the swap never removed it and polling never stopped.
func TestDetailPollInsideSwappedContent(t *testing.T) {
	c := &models.Container{DockerID: "abc123"}
	pending := renderDetail(t, models.ServiceStatusPending, c, nil)
	start, end := innerRange(t, pending, `id="service-detail-content"`)
	poll := strings.Index(pending, `id="service-detail-poll"`)
	if poll < 0 {
		t.Fatal("pending render has no poll")
	}
	if poll < start || poll > end {
		t.Fatalf("poll at %d is outside #service-detail-content [%d,%d]", poll, start, end)
	}
	if !strings.Contains(pending, `hx-select="#service-detail-content"`) || !strings.Contains(pending, `hx-target="#service-detail-content"`) {
		t.Fatal("poll no longer swaps #service-detail-content")
	}

	running := renderDetail(t, models.ServiceStatusRunning, c, nil)
	if strings.Contains(running, `id="service-detail-poll"`) || strings.Contains(running, `hx-trigger="every 3s"`) {
		t.Fatal("running render still carries the poll, so it would keep polling after the swap")
	}
}

// The stats stream is preserved across pending polls while the Docker ID is
// unchanged, and keyed by Docker ID so a recreate opens a new stream.
func TestDetailMetricsStreamPreservedByDockerID(t *testing.T) {
	html := renderDetail(t, models.ServiceStatusPending, &models.Container{DockerID: "abc123"}, nil)
	start, end := innerRange(t, html, `id="`+components.MetricsStreamID("abc123")+`"`)
	tag := html[start : strings.Index(html[start:], ">")+start]
	for _, want := range []string{`hx-preserve="true"`, `sse-connect="/containers/abc123/stats"`, `hx-ext="sse"`} {
		if !strings.Contains(tag, want) {
			t.Errorf("stream element lacks %s: %s", want, tag)
		}
	}
	if !strings.Contains(html[start:end], `sse-swap="stats,unavailable"`) {
		t.Error("stream does not swap the unavailable event")
	}
	other := renderDetail(t, models.ServiceStatusPending, &models.Container{DockerID: "def456"}, nil)
	if !strings.Contains(other, `id="metrics-stream-def456"`) || strings.Contains(other, "metrics-stream-abc123") {
		t.Error("stream id does not follow the Docker ID")
	}
}

func TestDetailContainerCard(t *testing.T) {
	started := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339Nano)
	s := &docker.InspectSummary{
		DockerID: "abc123", Image: "postgres:16-alpine", State: "running", Running: true,
		Health: "healthy", RestartCount: 2, OOMKilled: true, StartedAt: started,
		Ports:        []docker.InspectPort{{Container: "5432/tcp", HostIP: "127.0.0.1", HostPort: "15432"}},
		TraefikRules: map[string]string{"traefik.http.routers.pg.rule": "Host(`pg.example.test`)"},
		Limits:       docker.InspectLimits{MemoryBytes: 512 << 20, CPUs: 1.5},
		Volumes:      []docker.InspectMount{{Type: "volume", Name: "secret-vol-name", Destination: "/var/lib/postgresql/data"}},
		Networks:     []string{"goforge-net-hidden"},
	}
	html := renderDetail(t, models.ServiceStatusRunning, &models.Container{DockerID: "abc123"}, s)
	for _, want := range []string{
		"data-container-card", "postgres:16-alpine", "Running", "healthy", "Up 1h 30m",
		"OOM killed", ">Restarts<", "512.0 MiB memory, 1.5 CPUs", "127.0.0.1:15432 → 5432/tcp",
		"Host(`pg.example.test`)", `href="/containers/abc123"`, "View logs",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("container card lacks %q", want)
		}
	}
	// Volumes and networks are left out of the card (locked pick Q5).
	for _, absent := range []string{"secret-vol-name", "goforge-net-hidden"} {
		if strings.Contains(html, absent) {
			t.Errorf("container card shows %q", absent)
		}
	}

	none := renderDetail(t, models.ServiceStatusRunning, &models.Container{DockerID: "abc123"}, nil)
	if !strings.Contains(none, "Container details are not available") || !strings.Contains(none, `href="/containers/abc123"`) {
		t.Error("card without inspect should say so and keep the logs link")
	}
	if noCtr := renderDetail(t, models.ServiceStatusPending, nil, nil); strings.Contains(noCtr, "data-container-card") || strings.Contains(noCtr, "sse-connect") {
		t.Error("no container should render no card and no stream")
	}
}
