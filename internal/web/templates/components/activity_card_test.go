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

// t1683u: an empty card has no header "View all" and no table chrome; the
// empty message carries a secondary "View all activity" link instead.
func TestActivityCardEmptyHierarchy(t *testing.T) {
	html := renderActivityCard(t, nil)
	for _, want := range []string{`data-testid="recent-agent-actions-empty"`, "No recent agent actions", `href="/settings/activity"`, "View all activity"} {
		if !strings.Contains(html, want) {
			t.Errorf("empty card missing %q", want)
		}
	}
	for _, bad := range []string{"<table", ">Time<", "?source=agent", ">View all<"} {
		if strings.Contains(html, bad) {
			t.Errorf("empty card still has %q", bad)
		}
	}
	if strings.Index(html, "No recent agent actions") > strings.Index(html, "View all activity") {
		t.Error("View all activity must come after the empty message")
	}
}

func TestActivityCardRowsKeepHeaderAction(t *testing.T) {
	html := renderActivityCard(t, []ActivityRow{{ID: "a1", When: time.Unix(0, 0), Source: "api_v1", Action: "api_v1.restart_service"}})
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
