package projects

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

func renderOverview(t *testing.T, latest *models.Deployment, c *models.Container, s *docker.InspectSummary) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Overview(&models.Project{ID: "p1", Name: "app"}, latest, c, s).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestOverviewStreamsActiveContainerWithCard(t *testing.T) {
	latest := &models.Deployment{ID: "d1", Status: models.DeploymentStatusRunning}
	s := &docker.InspectSummary{DockerID: "abc123", Image: "goforge/app:d1", State: "running", Running: true, Health: "healthy"}
	html := renderOverview(t, latest, &models.Container{DockerID: "abc123", Status: models.ContainerStatusRunning}, s)
	for _, want := range []string{
		`sse-connect="/containers/abc123/stats"`, `id="metrics-stream-abc123"`, `sse-swap="stats,unavailable"`,
		"data-container-card", "goforge/app:d1", "healthy", `href="/containers/abc123"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("overview lacks %q", want)
		}
	}
}

func TestOverviewEmptyStateWithoutRunningContainer(t *testing.T) {
	latest := &models.Deployment{ID: "d2", Status: models.DeploymentStatusBuilding}
	html := renderOverview(t, latest, nil, nil)
	if strings.Contains(html, "sse-connect") {
		t.Fatal("overview streams with no running container")
	}
	if !strings.Contains(html, "No running container for this environment.") {
		t.Fatal("overview lacks the empty state")
	}
	if html := renderOverview(t, nil, nil, nil); strings.Contains(html, "data-resource-usage") {
		t.Fatal("project with no deployments should show no Resource Usage card")
	}
}
