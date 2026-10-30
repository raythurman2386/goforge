package settings

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/backup"
)

// Restore points render on the kit Table: a labeled, focusable scroll
// region, an sr-only caption, and scoped column headers.
func TestBackupRestorePointsTable(t *testing.T) {
	var b bytes.Buffer
	v := &backup.RestorePointsView{Points: []backup.RestorePoint{{RunKey: "2026-10-29/20261029T070000Z", Problem: "manifest version is newer"}}}
	if err := BackupRestorePoints(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		`role="region"`, `tabindex="0"`, `-caption"`, `class="sr-only">Restore points</caption>`,
		`data-testid="restore-points"`, `data-testid="restore-point"`, `colspan="4"`, "text-xs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("restore points table missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, `scope="col"`); n != 5 {
		t.Errorf("scope=col headers = %d, want 5", n)
	}
}
