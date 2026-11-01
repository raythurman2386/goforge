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
	if err := BackupCard(st, nil, "").Render(context.Background(), &b); err != nil {
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
		!strings.Contains(got, "app-db: boom") || !strings.Contains(got, "text-danger") {
		t.Errorf("failed card = %s", got)
	}

	st.LastRun, st.LastSuccess = nil, nil
	if got := render(t, st); !strings.Contains(got, "Last backup: never") {
		t.Errorf("never card = %s", got)
	}
}

func TestBackupCard_LazyRestorePoints(t *testing.T) {
	for _, st := range []*backup.Status{{Enabled: false}, {Enabled: true, Bucket: "b", Prefix: "p/"}} {
		got := render(t, st)
		if !strings.Contains(got, `hx-get="/settings/backups/restore-points"`) || !strings.Contains(got, `hx-trigger="load"`) {
			t.Errorf("card has no lazy restore points: %s", got)
		}
		if strings.Contains(got, "backup restore") && strings.Contains(got, "<button") {
			t.Errorf("card has a restore button: %s", got)
		}
	}
}

func TestBackupRestorePoints_EmptyAndProblem(t *testing.T) {
	var b bytes.Buffer
	if err := BackupRestorePoints(&backup.RestorePointsView{Bucket: "b", Prefix: "p/"}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "No complete backups in b/p/ yet.") {
		t.Errorf("empty = %s", b.String())
	}
	b.Reset()
	v := &backup.RestorePointsView{Points: []backup.RestorePoint{{RunKey: "2026-10-29/20261029T070000Z", Problem: "manifest version is newer"}}}
	if err := BackupRestorePoints(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "manifest version is newer") || !strings.Contains(b.String(), "backup restore 2026-10-29/20261029T070000Z") {
		t.Errorf("problem row = %s", b.String())
	}
}
