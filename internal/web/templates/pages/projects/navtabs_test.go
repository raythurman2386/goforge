package projects

import (
	"bytes"
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

var navTabsRE = regexp.MustCompile(`(?s)<nav aria-label="Project sections" data-ui-navtabs>(.*?)</nav>`)

// Project detail tabs are server-routed NavTabs: links (no tablist roles),
// exactly one aria-current="page", on the active tab.
func TestProjectDetailNavTabs(t *testing.T) {
	envs := []*models.Environment{{ID: "e1", Name: "production"}}
	now := time.Now()
	deps := []*models.Deployment{{ID: "d1", Status: models.DeploymentStatusRunning, CreatedAt: now,
		Branch: sql.NullString{String: "main", Valid: true}, CommitSHA: sql.NullString{String: "abcdef1234567", Valid: true}}}
	for _, tab := range []struct{ name, href string }{
		{"overview", `/projects/p1?env=e1`},
		{"deployments", `/projects/p1/deployments?env=e1`},
		{"services", `/projects/p1/services`},
		{"settings", `/projects/p1/settings?env=e1`},
	} {
		var buf bytes.Buffer
		err := Detail(&models.Project{ID: "p1", Name: "app"}, envs, envs[0], nil, deps, nil, nil, nil, nil, nil, tab.name, "tok").Render(context.Background(), &buf)
		if err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		m := navTabsRE.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("%s: no NavTabs nav", tab.name)
		}
		nav := m[1]
		if strings.Contains(nav, `role="tab`) {
			t.Errorf("%s: NavTabs must be links, not ARIA tabs", tab.name)
		}
		if n := strings.Count(nav, `aria-current="page"`); n != 1 {
			t.Errorf("%s: aria-current count = %d, want 1", tab.name, n)
		}
		cur := regexp.MustCompile(`<a aria-current="page"[^>]*href="` + regexp.QuoteMeta(tab.href) + `"`)
		if !cur.MatchString(nav) {
			t.Errorf("%s: active tab is not %s:\n%s", tab.name, tab.href, nav)
		}
		if strings.Contains(html, `aria-label="Tabs"`) {
			t.Errorf("%s: legacy tabs nav still rendered", tab.name)
		}
	}
}

// The Deployments tab renders its table on the kit Table.
func TestProjectDeploymentsTable(t *testing.T) {
	deps := []*models.Deployment{{ID: "d1", Status: models.DeploymentStatusRunning, CreatedAt: time.Now()}}
	var buf bytes.Buffer
	if err := Deployments(&models.Project{ID: "p1", Name: "app"}, deps).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{`role="region"`, `tabindex="0"`, `class="sr-only">Deployments of app</caption>`, `href="/projects/p1/deployments/d1"`} {
		if !strings.Contains(got, want) {
			t.Errorf("deployments table missing %q", want)
		}
	}
	if n := strings.Count(got, `scope="col"`); n != 5 {
		t.Errorf("scope=col headers = %d, want 5", n)
	}
}
