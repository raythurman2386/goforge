package apiv1

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
)

type fakeBackups struct{}

func (fakeBackups) Status(context.Context) (*backup.Status, error) {
	next := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	run := backup.RunView{ID: "r1", Trigger: "schedule", Status: "succeeded", RunKey: "2026-10-29/20261029T070000Z", Objects: 3}
	return &backup.Status{
		Enabled: true, Bucket: "goforge-backups", Prefix: "goforge/", Schedule: "02:00 America/Chicago",
		RetentionDaily: 7, NextRunAt: &next, LastRun: &run, LastSuccess: &run, Recent: []backup.RunView{run},
	}, nil
}

func TestGetBackups(t *testing.T) {
	h := newHarness(t)

	rec, out := h.do(t, http.MethodGet, "/backups", "tok-read", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin: got %d (%v)", rec.Code, out)
	}

	rec, out = h.do(t, http.MethodGet, "/backups", "tok-admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: got %d (%v)", rec.Code, out)
	}
	if out["enabled"] != true || out["schedule"] != "02:00 America/Chicago" || out["retention_daily"] != float64(7) {
		t.Errorf("body = %v", out)
	}
	last, _ := out["last_success"].(map[string]any)
	if last["run_key"] != "2026-10-29/20261029T070000Z" {
		t.Errorf("last_success = %v", out["last_success"])
	}
	body := strings.ToLower(rec.Body.String())
	for _, bad := range []string{"secret", "access_key", "endpoint"} {
		if strings.Contains(body, bad) {
			t.Errorf("backup status mentions %q: %s", bad, body)
		}
	}

	rec, _ = h.do(t, http.MethodPost, "/backups", "tok-admin", "")
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Errorf("POST /backups must not exist in v1: got %d", rec.Code)
	}
}
