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
	if err := Profile(&models.User{Email: "u@x", Role: models.RoleUser}, "tok", nil, nil).Render(context.Background(), &b); err != nil {
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
