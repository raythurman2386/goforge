package settings

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestBackupCard_FormNeverShowsSecret(t *testing.T) {
	const secret = "wJalrXUtnFEMI-K7MDENG-bPxRfiCYEXAMPLEKEY" //nolint:gosec // AWS documentation example, not a credential
	const keyID = "AKIAIOSFODNN7EXAMPLE"                      //nolint:gosec // AWS documentation example, not a credential
	view := backup.NewSettingsView(config.BackupConfig{
		Endpoint: "https://s3.example.com", Region: "auto", Bucket: "bk", Prefix: "goforge/",
		AccessKeyID: keyID, SecretAccessKey: secret, Schedule: "02:00 America/Chicago",
		RetentionDaily: 7, Timeout: 90 * time.Minute,
	}, backup.SourceSettings)
	var b bytes.Buffer
	if err := BackupCard(&backup.Status{Enabled: true, Bucket: "bk", Prefix: "goforge/", Schedule: "02:00 America/Chicago"}, view, "tok").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.Contains(got, secret) || strings.Contains(got, keyID[:len(keyID)-4]) {
		t.Fatalf("form leaks a key: %s", got)
	}
	for _, want := range []string{
		"ending in MPLE", `name="secret_access_key"`, `type="password"`, "Set. Leave blank to keep it",
		`hx-put="/settings/backups"`, `hx-post="/settings/backups/test"`, `value="1h30m"`, `value="02:00"`, `value="America/Chicago"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("form missing %q", want)
		}
	}
}

func TestBackupCard_ShortKeyIDNotShown(t *testing.T) {
	view := backup.NewSettingsView(config.BackupConfig{Bucket: "bk", Region: "auto", AccessKeyID: "shortkey"}, backup.SourceEnvironment)
	var b bytes.Buffer
	if err := BackupCard(&backup.Status{}, view, "tok").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); strings.Contains(got, "shortkey") || strings.Contains(got, "tkey") || !strings.Contains(got, "Secret access key") {
		t.Fatalf("short key id shown: %s", got)
	}
}

func TestProfile_NonAdminHasNoBackupCard(t *testing.T) {
	var b bytes.Buffer
	if err := Profile(&models.User{Email: "u@x", Role: models.RoleUser}, "tok", nil, nil, nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.Contains(got, "backup-status") || strings.Contains(got, "/settings/backups") {
		t.Fatal("non-admin sees backup card")
	}
	if strings.Contains(got, "planned for future releases") || !strings.Contains(got, "Authorization: Bearer") {
		t.Error("API token alert not updated")
	}
}

func TestBackupCard_UnreadableKeysAsksForBothKeys(t *testing.T) {
	view := backup.NewSettingsView(config.BackupConfig{
		Endpoint: "https://s3.example.com", Region: "auto", Bucket: "bk", Prefix: "goforge/",
		Schedule: "02:00 America/Chicago", RetentionDaily: 7, Timeout: 2 * time.Hour,
	}, backup.SourceSettings)
	view.KeysUnreadable = true
	var b bytes.Buffer
	if err := BackupCard(&backup.Status{ConfiguredBy: backup.SourceSettings}, view, "tok").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"the saved keys do not decrypt with the current ENCRYPTION_KEY", "Enter both keys again below",
		"does not decrypt with the current ENCRYPTION_KEY. Enter it again.", `value="bk"`, `value="https://s3.example.com"`,
		`hx-put="/settings/backups"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("card missing %q", want)
		}
	}
	if strings.Contains(got, "BACKUP_S3_BUCKET") || strings.Contains(got, "Leave blank to keep") {
		t.Fatalf("card points at env or promises to keep unreadable keys: %s", got)
	}
}

func TestBackupCard_OffLineDoesNotPointAtEnvOnceSaved(t *testing.T) {
	var b bytes.Buffer
	if err := BackupCard(&backup.Status{ConfiguredBy: backup.SourceSettings}, nil, "").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); strings.Contains(got, "BACKUP_S3_BUCKET") || !strings.Contains(got, "could not be loaded") {
		t.Fatalf("off line with saved settings: %s", got)
	}
	b.Reset()
	if err := BackupCard(&backup.Status{ConfiguredBy: backup.SourceEnvironment}, nil, "").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !strings.Contains(got, "BACKUP_S3_BUCKET") {
		t.Fatalf("off line from env: %s", got)
	}
}
