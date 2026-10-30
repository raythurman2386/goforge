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

var (
	tagRE   = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)([^>]*?)(/?)>`)
	everyRE = regexp.MustCompile(`hx-trigger="[^"]*every `)
	voidEl  = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}
)

// liveRegion reports whether an element's attributes make htmx replace it
// on a timer (hx-trigger "every Ns") or over SSE.
func liveRegion(attrs string) bool {
	return everyRE.MatchString(attrs) ||
		strings.Contains(attrs, "sse-swap=") || strings.Contains(attrs, "sse-connect=")
}

// overlaysInLiveRegions walks the rendered markup and returns the opening
// tag of every popover overlay that has a polled or SSE ancestor.
func overlaysInLiveRegions(html string) (found int, bad []string) {
	type open struct {
		name string
		live bool
	}
	var stack []open
	for _, m := range tagRE.FindAllStringSubmatch(html, -1) {
		closing, name, attrs, self := m[1] == "/", strings.ToLower(m[2]), m[3], m[4] == "/"
		if closing {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == name {
					stack = stack[:i]
					break
				}
			}
			continue
		}
		if strings.Contains(attrs, " popover=") {
			found++
			for _, o := range stack {
				if o.live {
					bad = append(bad, m[0])
					break
				}
			}
		}
		if !self && !voidEl[name] {
			stack = append(stack, open{name, liveRegion(attrs)})
		}
	}
	return found, bad
}

// Rule 8 of the UI kit runtime: no overlay lives inside a region htmx
// polls or swaps over SSE, or the swap destroys it while it is open. The
// project page has both (the 3s deployment banner and the SSE stats card).
func TestProjectDetailOverlaysOutsideLiveRegions(t *testing.T) {
	envs := []*models.Environment{{ID: "e1", Name: "production"}, {ID: "e2", Name: "staging"}}
	active := &models.Deployment{ID: "d1", Status: models.DeploymentStatusBuilding}
	running := &models.Deployment{ID: "d0", Status: models.DeploymentStatusRunning}
	summary := &docker.InspectSummary{DockerID: "abc123", Image: "app:d0", State: "running", Running: true}
	var buf bytes.Buffer
	err := Detail(&models.Project{ID: "p1", Name: "app"}, envs, envs[0], active, []*models.Deployment{active, running},
		&models.Container{DockerID: "abc123", Status: models.ContainerStatusRunning}, summary,
		nil, nil, nil, "overview", "tok").Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `hx-trigger="every 3s"`) || !strings.Contains(html, "sse-connect=") {
		t.Fatal("fixture should render both the polled banner and the SSE region")
	}
	found, bad := overlaysInLiveRegions(html)
	if found == 0 {
		t.Fatal("expected the environment menu popover on the page")
	}
	for _, b := range bad {
		t.Errorf("overlay inside a polled or SSE region: %s", b)
	}
}

func TestOverlaysInLiveRegionsDetects(t *testing.T) {
	_, bad := overlaysInLiveRegions(`<div hx-trigger="every 3s"><span><div popover="auto" id="x"></div></span></div><div popover="manual"></div>`)
	if len(bad) != 1 {
		t.Fatalf("bad = %v, want exactly the polled one", bad)
	}
}
