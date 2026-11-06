package handlers

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		{"active actions", func(w *httptest.ResponseRecorder) {
			writeDeploymentActions(w, w, r, &models.Deployment{ID: "d", EnvironmentID: "e", Status: models.DeploymentStatusBuilding}, "p", false, "csrf")
		}, `data: <div id="deployment-actions" hx-swap-oob="innerHTML">`},
		{"terminal actions", func(w *httptest.ResponseRecorder) {
			writeDeploymentActions(w, w, r, &models.Deployment{ID: "d", EnvironmentID: "e", Status: models.DeploymentStatusRunning}, "p", true, "csrf")
		}, `data: <div id="deployment-actions" hx-swap-oob="innerHTML">`},
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
	writeDeploymentProgress(w, w, r, &models.Deployment{Status: models.DeploymentStatusBuilding, StartedAt: sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true}})
	body := w.Body.String()
	if strings.Contains(body, "event:") || !strings.Contains(body, `data: <div id="deployment-progress" hx-swap-oob="innerHTML">`) || !strings.Contains(body, `data-deployment-stage="building"`) || !strings.Contains(body, "Elapsed: 1m 0s") {
		t.Errorf("progress is not an unnamed OOB stage update:\n%s", body)
	}
	w = httptest.NewRecorder()
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

func TestDeploymentActionsSwapCancelForEligibleRollback(t *testing.T) {
	r := httptest.NewRequest("GET", "/projects/p/deployments/d/logs", nil)
	w := httptest.NewRecorder()
	writeDeploymentActions(w, w, r, &models.Deployment{ID: "d", EnvironmentID: "e", Status: models.DeploymentStatusBuilding}, "p", false, "csrf")
	active := w.Body.String()
	if !strings.Contains(active, "Cancel<svg") || strings.Contains(active, "Rollback<svg") {
		t.Fatalf("active actions = %q", active)
	}
	w = httptest.NewRecorder()
	writeDeploymentActions(w, w, r, &models.Deployment{ID: "d", EnvironmentID: "e", Status: models.DeploymentStatusRunning}, "p", true, "csrf")
	terminal := w.Body.String()
	if !strings.Contains(terminal, "Rollback<svg") || strings.Contains(terminal, "Cancel<svg") || !strings.Contains(terminal, `hx-post="/projects/p/deployments/d/rollback"`) {
		t.Fatalf("terminal actions = %q", terminal)
	}
}
