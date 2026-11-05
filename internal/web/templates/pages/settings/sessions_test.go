package settings

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestSessionRows(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	var b bytes.Buffer
	err := SessionRows([]*models.Session{
		{ID: "current", CreatedAt: now, LastActiveAt: now},
		{ID: "other", CreatedAt: now.Add(-time.Hour), LastActiveAt: now},
	}, "current", "csrf-test").Render(context.Background(), &b)
	if err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if n := strings.Count(got, `data-testid="session-row"`); n != 2 {
		t.Fatalf("session rows = %d, want 2", n)
	}
	for _, want := range []string{"Browser session", "This browser", "Other session", "Started Nov 5, 2026", `hx-post="/settings/sessions/other/revoke"`, `name="csrf_token" value="csrf-test"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, "card") || strings.Contains(got, "hx-delete") || strings.Contains(got, "/settings/sessions/current/revoke") {
		t.Error("session rows introduced a card, unsafe method, or current-session action")
	}
	if n := strings.Count(got, `data-testid="revoke-session"`); n != 1 {
		t.Errorf("per-row sign-out actions = %d, want 1", n)
	}
}
