package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func serviceBackupForm(on bool) url.Values {
	if on {
		return url.Values{"backup_enabled": {"true", "false"}}
	}
	return url.Values{"backup_enabled": {"false"}}
}

// Turning the toggle on stores the flag, writes one service.set_backup row
// (via=ui), and re-renders the card checked. Turning it off does the
// reverse. Posting the current value writes nothing.
func TestServiceHandler_SetBackupEnabled_TogglesAndAudits(t *testing.T) {
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.SetBackupEnabled(rr, editRequest(http.MethodPost, "owner", serviceBackupForm(true)))
	if rr.Code != http.StatusOK {
		t.Fatalf("on: status = %d body=%s", rr.Code, rr.Body)
	}
	if !repo.inst.BackupEnabled {
		t.Fatal("flag not stored")
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="service-backup"`) || !strings.Contains(body, " checked") || !strings.Contains(body, "Included in nightly backups") {
		t.Fatalf("card not re-rendered checked: %s", body)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rr.Header().Get("Cache-Control"))
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.action != models.AuditActionServiceSetBackup || r.resourceType != models.ResourceTypeServiceInstance ||
		r.resourceID != editTestInstanceID || r.userID != "owner" {
		t.Errorf("row = %+v", r)
	}
	if r.details["via"] != models.AuditViaUI || r.details["backup_enabled"] != true || r.details["previous"] != false || r.details["name"] != "vw" {
		t.Errorf("details = %v", r.details)
	}

	rr = httptest.NewRecorder()
	h.SetBackupEnabled(rr, editRequest(http.MethodPost, "owner", serviceBackupForm(true)))
	if rr.Code != http.StatusOK || len(rec.snapshot()) != 1 {
		t.Fatalf("no-op: status=%d rows=%d", rr.Code, len(rec.snapshot()))
	}

	rr = httptest.NewRecorder()
	h.SetBackupEnabled(rr, editRequest(http.MethodPost, "owner", serviceBackupForm(false)))
	if rr.Code != http.StatusOK || repo.inst.BackupEnabled {
		t.Fatalf("off: status=%d enabled=%v", rr.Code, repo.inst.BackupEnabled)
	}
	if !strings.Contains(rr.Body.String(), "Left out of nightly backups") {
		t.Fatalf("off card: %s", rr.Body)
	}
	rows = rec.snapshot()
	if len(rows) != 2 || rows[1].details["backup_enabled"] != false || rows[1].details["previous"] != true {
		t.Fatalf("off rows = %+v", rows)
	}
}

func TestServiceHandler_SetBackupEnabled_NonOwnerRefused(t *testing.T) {
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.SetBackupEnabled(rr, editRequest(http.MethodPost, "intruder", serviceBackupForm(true)))
	if rr.Code != http.StatusForbidden || repo.inst.BackupEnabled || len(rec.snapshot()) != 0 {
		t.Fatalf("status=%d enabled=%v rows=%d", rr.Code, repo.inst.BackupEnabled, len(rec.snapshot()))
	}
}

// A template the runner does not dump cannot be opted in.
func TestServiceHandler_SetBackupEnabled_UnsupportedTemplate(t *testing.T) {
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	repo.inst.TemplateName = "redis"
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.SetBackupEnabled(rr, editRequest(http.MethodPost, "owner", serviceBackupForm(true)))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "no nightly backup") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body)
	}
	if repo.inst.BackupEnabled || len(rec.snapshot()) != 0 {
		t.Fatal("refused toggle changed state or wrote audit")
	}
}
