package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

// htmx 4 hx-sse swaps only unnamed messages; a named event is just a DOM
// event. Each deployment log message is unnamed and carries one out-of-band
// swap, since the connect element itself swaps nothing.
func TestDeploymentStreamMessagesAreUnnamedOOBSwaps(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/p/deployments/d/logs", nil)
	line := &models.DeploymentLog{ID: "l1", Level: models.DeploymentLogLevel("info"), Message: "built <ok>"}
	for _, tc := range []struct {
		name  string
		write func(*httptest.ResponseRecorder)
		want  string
	}{
		{"snapshot", func(w *httptest.ResponseRecorder) {
			writeDeploymentSnapshot(w, w, r, []*models.DeploymentLog{line})
		}, `data: <div id="deployment-log-lines" hx-swap-oob="innerHTML">`},
		{"empty snapshot", func(w *httptest.ResponseRecorder) { writeDeploymentSnapshot(w, w, r, nil) },
			`data: <div id="deployment-log-lines" hx-swap-oob="innerHTML"><p class="text-foreground-secondary italic">No logs available yet...</p></div>`},
		{"log", func(w *httptest.ResponseRecorder) { writeDeploymentLog(w, w, r, line) },
			`data: <div hx-swap-oob="beforeend:#deployment-log-lines">`},
		{"status", func(w *httptest.ResponseRecorder) { writeDeploymentStatus(w, w, r, models.DeploymentStatusRunning) },
			`data: <span id="deployment-live-status" hx-swap-oob="innerHTML">`},
	} {
		w := httptest.NewRecorder()
		tc.write(w)
		body := w.Body.String()
		if strings.Contains(body, "event:") {
			t.Errorf("%s: sent as a named event:\n%s", tc.name, body)
		}
		if !strings.HasPrefix(body, tc.want) || !strings.HasSuffix(body, "\n\n") {
			t.Errorf("%s: got\n%s\nwant prefix %s", tc.name, body, tc.want)
		}
	}
	w := httptest.NewRecorder()
	writeDeploymentLog(w, w, r, line)
	if !strings.Contains(w.Body.String(), "built &lt;ok&gt;") {
		t.Errorf("log line not escaped:\n%s", w.Body.String())
	}
}

// Container log lines append to the stream element (hx-swap="beforeend").
func TestContainerLogLineIsUnnamedMessage(t *testing.T) {
	w := httptest.NewRecorder()
	writeContainerLogLine(w, w, "a <b>")
	if got, want := w.Body.String(), "data: <div class=\"py-0.5\">a &lt;b&gt;</div>\n\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
