package settings

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
)

func render(t *testing.T, st *backup.Status) string {
	t.Helper()
	var b bytes.Buffer
	if err := BackupCard(st).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestBackupCard(t *testing.T) {
	if got := render(t, &backup.Status{Enabled: false}); !strings.Contains(got, "Backups are off") {
		t.Errorf("disabled card = %s", got)
	}

	started := time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC)
	ok := backup.RunView{Status: "succeeded", StartedAt: started, Objects: 9, Bytes: 5 << 20}
	st := &backup.Status{Enabled: true, Bucket: "b", Prefix: "goforge/", Schedule: "02:00 America/Chicago", RetentionDaily: 7, LastRun: &ok, LastSuccess: &ok}
	got := render(t, st)
	if !strings.Contains(got, "Last backup: Oct 29, 2026 02:00 CDT, 9 objects, 5.0 MiB.") {
		t.Errorf("success card = %s", got)
	}

	failed := backup.RunView{Status: "failed", StartedAt: started.Add(24 * time.Hour), ErrorSummary: "1 step(s) failed: app-db: boom"}
	st.LastRun = &failed
	got = render(t, st)
	if !strings.Contains(got, "Last backup: failed Oct 30, 2026 02:00 CDT. Last success Oct 29, 2026 02:00 CDT (5.0 MiB).") ||
		!strings.Contains(got, "app-db: boom") || !strings.Contains(got, "text-red-700") {
		t.Errorf("failed card = %s", got)
	}

	st.LastRun, st.LastSuccess = nil, nil
	if got := render(t, st); !strings.Contains(got, "Last backup: never") {
		t.Errorf("never card = %s", got)
	}
}
