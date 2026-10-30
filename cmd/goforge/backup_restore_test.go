package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
)

func TestConfirmRestore(t *testing.T) {
	const key = "2026-10-29/20261029T070000Z"
	t.Cleanup(func() { restoreConfirm = "" })
	restoreConfirm = key
	if err := confirmRestore(key, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("matching --confirm: %v", err)
	}
	restoreConfirm = "2026-10-28/20261028T070000Z"
	if err := confirmRestore(key, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("mismatched --confirm accepted")
	}
	// No --confirm and no terminal: refuse rather than read a piped answer.
	restoreConfirm = ""
	if err := confirmRestore(key, strings.NewReader(key+"\n"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("non-terminal: %v", err)
	}
}

func TestRunBackupRestore_RejectsBadKeyAndConfirmWithoutYes(t *testing.T) {
	t.Cleanup(func() { restoreYes, restoreConfirm = false, "" })
	if err := runBackupRestore(context.Background(), "../etc", strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("bad run key accepted")
	}
	restoreConfirm = "2026-10-29/20261029T070000Z"
	if err := runBackupRestore(context.Background(), "2026-10-29/20261029T070000Z", strings.NewReader(""), &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "--yes") {
		t.Fatalf("--confirm without --yes: %v", err)
	}
}

func TestWriteRestorePoints(t *testing.T) {
	m := &backup.Manifest{Trigger: "manual", GoForgeVersion: "0.1.0", StartedAt: time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC),
		Artifacts: []backup.ManifestArtifact{{Bytes: 100}, {Bytes: 23}}, Skipped: []backup.Skipped{{}}}
	chi, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip(err)
	}
	var b bytes.Buffer
	writeRestorePoints(&b, []backup.RestorePoint{
		{RunKey: "2026-10-29/20261029T070000Z", Manifest: m},
		{RunKey: "2026-10-28/20261028T070000Z", Problem: "manifest version is newer"},
	}, chi)
	out := b.String()
	for _, want := range []string{"RUN KEY", "2026-10-29/20261029T070000Z", "manual", "0.1.0", "2026-10-29 02:00:00 CDT", "123", "manifest version is newer"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
