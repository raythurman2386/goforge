package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/models"
)

type fakeRestorePoints struct {
	view  *backup.RestorePointsView
	calls int
}

func (f *fakeRestorePoints) List(context.Context) *backup.RestorePointsView {
	f.calls++
	return f.view
}

func restorePointsView() *backup.RestorePointsView {
	m := &backup.Manifest{Version: 1, RunKey: "2026-10-29/20261029T070000Z", Trigger: "scheduled",
		StartedAt: time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC),
		Artifacts: []backup.ManifestArtifact{{Key: backup.AppDumpKey, Bytes: 2048}}}
	return &backup.RestorePointsView{Bucket: "bk", Prefix: "goforge/", Schedule: "02:00 America/Chicago",
		Points: []backup.RestorePoint{{RunKey: m.RunKey, Manifest: m}}, Incomplete: 1}
}

func TestRestorePoints_AdminOnly(t *testing.T) {
	f := &fakeRestorePoints{view: restorePointsView()}
	h := backupHandler(&fakeBackupSettings{}, nil).WithRestorePoints(f)
	for _, tc := range []struct {
		user *models.User
		want int
	}{{nil, http.StatusUnauthorized}, {plainUser, http.StatusForbidden}} {
		rr := httptest.NewRecorder()
		h.RestorePoints(rr, backupReq(http.MethodGet, backupPath+"/restore-points", nil, tc.user))
		if rr.Code != tc.want {
			t.Errorf("status = %d, want %d", rr.Code, tc.want)
		}
	}
	if f.calls != 0 {
		t.Fatal("non-admin listed the bucket")
	}
}

func TestRestorePoints_ReadOnlyList(t *testing.T) {
	f := &fakeRestorePoints{view: restorePointsView()}
	rr := httptest.NewRecorder()
	backupHandler(&fakeBackupSettings{}, nil).WithRestorePoints(f).
		RestorePoints(rr, backupReq(http.MethodGet, backupPath+"/restore-points", nil, adminUser))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"Restore points", "2026-10-29/20261029T070000Z", "Oct 29, 2026 02:00 CDT", "scheduled", "2.0 KiB",
		"1 incomplete run(s)", "./goforge backup restore 2026-10-29/20261029T070000Z", "docker compose stop goforge",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	// Read-only: no form, button, or mutating htmx verb.
	for _, bad := range []string{"<form", "<button", "hx-post", "hx-put", "hx-delete"} {
		if strings.Contains(body, bad) {
			t.Errorf("restore points fragment contains %q", bad)
		}
	}
}

func TestRestorePoints_ErrorAndUnwired(t *testing.T) {
	f := &fakeRestorePoints{view: &backup.RestorePointsView{Bucket: "bk", Error: "Could not list the bucket: Access Denied"}}
	rr := httptest.NewRecorder()
	backupHandler(&fakeBackupSettings{}, nil).WithRestorePoints(f).
		RestorePoints(rr, backupReq(http.MethodGet, backupPath+"/restore-points", nil, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Could not list the bucket: Access Denied") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	rr = httptest.NewRecorder()
	backupHandler(&fakeBackupSettings{}, nil).RestorePoints(rr, backupReq(http.MethodGet, backupPath+"/restore-points", nil, adminUser))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unwired status = %d", rr.Code)
	}
}
