package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func renderActivityCard(t *testing.T, rows []ActivityRow) string {
	t.Helper()
	var buf bytes.Buffer
	if err := ActivityCard(rows).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// cardHeader is the card-header div of a rendered card.
func cardHeader(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `class="card-header"`)
	if start < 0 {
		t.Fatal("card has no card-header")
	}
	end := strings.Index(html[start:], `</p></div>`)
	if end < 0 {
		t.Fatal("card-header has no title block")
	}
	// The header closes after the action that follows the title block.
	rest := html[start+end:]
	return html[start : start+end+strings.Index(rest, `</a></div>`)+len(`</a></div>`)]
}

// t1695u: both states are the kit's Flush card: a card-header whose direct
// children are the title block and one ghost "View all" action (no wrapper
// that shrinks and leaves the action floating mid-row).
func TestActivityCardKitHeader(t *testing.T) {
	for name, rows := range map[string][]ActivityRow{
		"empty": nil,
		"rows":  {{ID: "a1", When: time.Unix(0, 0), Source: "api", Action: "api_v1.restart_service"}},
	} {
		t.Run(name, func(t *testing.T) {
			html := renderActivityCard(t, rows)
			header := cardHeader(t, html)
			if !strings.HasPrefix(header, `class="card-header"><div class="min-w-0"><h2 class="card-title">Recent agent actions`) {
				t.Errorf("header is not title block first: %s", header)
			}
			if !strings.Contains(header, `</p></div><a href=`) {
				t.Errorf("View all must follow the title block as the header's second child: %s", header)
			}
			if strings.Count(header, `data-testid="recent-agent-actions-view-all"`) != 1 || !strings.Contains(header, "btn-ghost btn-sm") {
				t.Errorf("header needs exactly one ghost sm View all action: %s", header)
			}
			if strings.Contains(header, "justify-between") {
				t.Error("header must use card-header's own layout, not a nested flex row")
			}
			if strings.Count(html, "recent-agent-actions-view-all") != 1 {
				t.Error("View all must appear once, in the header")
			}
		})
	}
}

// t1683u/t1695u: an empty card has no table chrome; its body is a compact
// EmptyState in CardContent and its header action goes to all activity.
func TestActivityCardEmptyHierarchy(t *testing.T) {
	html := renderActivityCard(t, nil)
	for _, want := range []string{`class="card-body" data-testid="recent-agent-actions-empty"`, "No recent agent actions", `href="/settings/activity"`, "View all activity"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty card missing %q", want)
		}
	}
	for _, bad := range []string{"<table", ">Time<", "?source=agent", "-mt-4", "btn-link"} {
		if strings.Contains(html, bad) {
			t.Errorf("empty card still has %q", bad)
		}
	}
	if strings.Index(html, "View all activity") > strings.Index(html, "No recent agent actions") {
		t.Error("View all activity belongs in the header, before the empty message")
	}
}

func TestActivityCardRowsKeepHeaderAction(t *testing.T) {
	html := renderActivityCard(t, []ActivityRow{{ID: "a1", When: time.Unix(0, 0), Source: "api", Action: "api_v1.restart_service"}})
	for _, want := range []string{"<table", `href="/settings/activity?source=agent"`, "api_v1.restart_service"} {
		if !strings.Contains(html, want) {
			t.Errorf("card with rows missing %q", want)
		}
	}
	if strings.Contains(html, "recent-agent-actions-empty") || strings.Contains(html, "View all activity") {
		t.Error("card with rows shows the empty state")
	}
	if title, link := strings.Index(html, "Recent agent actions"), strings.Index(html, "?source=agent"); link < title || link > strings.Index(html, "<table") {
		t.Error("View all must sit in the header, after the title and before the table")
	}
}
