package projects

import (
	"bytes"
	"context"
	"regexp"
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

// listTagRe matches the open and close tags that decide dt/dd nesting.
var listTagRe = regexp.MustCompile(`<(/?)(dl|dt|dd|div)[\s>]`)

// dlItemsOutsideDL counts dt and dd elements whose parent is not a dl, or a
// div whose parent is a dl (axe rule dlitem).
func dlItemsOutsideDL(html string) int {
	stack := make([]string, 0, 16)
	bad := 0
	for _, m := range listTagRe.FindAllStringSubmatch(html, -1) {
		closing, tag := m[1] == "/", m[2]
		if closing {
			if n := len(stack); n > 0 {
				stack = stack[:n-1]
			}
			continue
		}
		if tag == "dt" || tag == "dd" {
			n := len(stack)
			ok := n > 0 && stack[n-1] == "dl" || n > 1 && stack[n-1] == "div" && stack[n-2] == "dl"
			if !ok {
				bad++
			}
		}
		stack = append(stack, tag)
	}
	return bad
}

// The Overview description list must keep every dt/dd under a dl, directly or
// through one div row wrapper, so axe dlitem stays at zero.
func TestOverviewDescriptionListNesting(t *testing.T) {
	html := renderOverview(t, nil, nil, nil)
	if !strings.Contains(html, "<dt") {
		t.Fatal("overview lacks the description list")
	}
	if n := dlItemsOutsideDL(html); n != 0 {
		t.Fatalf("overview has %d dt/dd outside a dl", n)
	}
	if n := dlItemsOutsideDL(`<div><div><dt>a</dt><dd>b</dd></div></div>`); n != 2 {
		t.Fatalf("checker missed bare dt/dd: got %d, want 2", n)
	}
}
