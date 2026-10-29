package backup

import (
	"testing"
	"time"
)

func TestRunKey(t *testing.T) {
	chi, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	// 02:00 CDT on Oct 29 is 07:00 UTC; the date folder follows the schedule
	// zone, the run id is UTC.
	started := time.Date(2026, 10, 29, 2, 0, 5, 0, chi)
	if got, want := RunKey(started, chi), "2026-10-29/20261029T070005Z"; got != want {
		t.Errorf("RunKey() = %q, want %q", got, want)
	}
	// 23:30 Chicago is already the next day in UTC.
	late := time.Date(2026, 10, 29, 23, 30, 0, 0, chi)
	if got, want := RunKey(late, chi), "2026-10-29/20261030T043000Z"; got != want {
		t.Errorf("RunKey() = %q, want %q", got, want)
	}
	if got, want := RunKey(late, nil), "2026-10-30/20261030T043000Z"; got != want {
		t.Errorf("RunKey(nil loc) = %q, want %q", got, want)
	}
	if !runPathRe.MatchString(RunKey(started, chi) + "/" + ManifestName) {
		t.Error("RunKey output does not match the retention pattern")
	}
}

func TestObjectKeyAndSafeSegment(t *testing.T) {
	if got := ObjectKey("goforge/", "2026-10-29/20261029T070005Z", "/app/x.dump"); got != "goforge/2026-10-29/20261029T070005Z/app/x.dump" {
		t.Errorf("ObjectKey() = %q", got)
	}
	for in, want := range map[string]string{
		"app":       "app",
		"my db":     "my_db",
		"../etc":    "_etc",
		"..":        "_",
		"a/b":       "a_b",
		"Ünïcode-1": "_n_code-1",
	} {
		if got := safeSegment(in); got != want {
			t.Errorf("safeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactor(t *testing.T) {
	r := NewRedactor("supersecretkey", "", "abc")
	got := r.Redact("dial postgres://goforge:hunter2@db:5432/x failed;\nkey=supersecretkey abc")
	want := "dial postgres://goforge:[redacted]@db:5432/x failed; key=[redacted] abc"
	if got != want {
		t.Errorf("Redact() = %q, want %q", got, want)
	}
	var nilR *Redactor
	if got := nilR.Redact("x://u:p@h"); got != "x://u:[redacted]@h" {
		t.Errorf("nil Redactor = %q", got)
	}
}
