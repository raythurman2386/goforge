package pages_services //nolint:revive,stylecheck // matches the templ package name

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func renderBackup(t *testing.T, b BackupChoice) string {
	t.Helper()
	var buf bytes.Buffer
	if err := BackupCard(b).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestBackupCardStates(t *testing.T) {
	if got := renderBackup(t, BackupChoice{InstanceID: "inst-1"}); got != "" {
		t.Fatalf("template with no backup rendered a card: %s", got)
	}

	off := renderBackup(t, BackupChoice{InstanceID: "inst-1", Offered: true})
	for _, want := range []string{
		`hx-post="/services/inst-1/backup"`,
		`name="backup_enabled"`,
		`value="true"`,
		`value="false"`,
		"Allows backups",
		"Left out of nightly backups",
	} {
		if !strings.Contains(off, want) {
			t.Errorf("off card missing %q", want)
		}
	}
	if strings.Contains(off, " checked") {
		t.Errorf("off card should be unchecked: %s", off)
	}

	on := renderBackup(t, BackupChoice{InstanceID: "inst-1", Offered: true, Enabled: true})
	if !strings.Contains(on, " checked") || !strings.Contains(on, "Included in nightly backups") {
		t.Errorf("on card should be checked: %s", on)
	}
}

func TestDetailBackupCardOnlyWhenOffered(t *testing.T) {
	if html := renderDetail(t, models.ServiceStatusRunning, nil, nil); strings.Contains(html, `id="service-backup"`) {
		t.Fatal("detail rendered Allows backups without a backup choice")
	}
}
