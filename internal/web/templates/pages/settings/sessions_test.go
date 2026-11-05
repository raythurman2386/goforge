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
	}, "current").Render(context.Background(), &b)
	if err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if n := strings.Count(got, `data-testid="session-row"`); n != 2 {
		t.Fatalf("session rows = %d, want 2", n)
	}
	for _, want := range []string{"Browser session", "This browser", "Other session", "Started Nov 5, 2026"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, "card") || strings.Contains(got, "hx-delete") {
		t.Error("session rows introduced a card or a new mutation endpoint")
	}
}
