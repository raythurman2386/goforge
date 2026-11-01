package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

type uiAuditRow struct {
	id, userID, action, resourceType, resourceID string
	details                                      map[string]interface{}
}

// fakeUIAudit records Record and RecordAs calls synchronously.
type fakeUIAudit struct {
	mu   sync.Mutex
	rows []uiAuditRow
}

func (f *fakeUIAudit) Record(ctx context.Context, userID, action, rt, rid string, d map[string]interface{}, _, _ string) {
	f.RecordAs(ctx, "", userID, action, rt, rid, d, "", "")
}

func (f *fakeUIAudit) RecordAs(_ context.Context, id, userID, action, rt, rid string, d map[string]interface{}, _, _ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, uiAuditRow{id, userID, action, rt, rid, d})
}

func (f *fakeUIAudit) snapshot() []uiAuditRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uiAuditRow(nil), f.rows...)
}

// Deleting a service in the UI writes service.delete for that instance.
func TestServiceHandler_Delete_WritesAuditRow(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.Delete(rr, editRequest(http.MethodDelete, "owner", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.action != models.AuditActionServiceDelete || r.resourceType != models.ResourceTypeServiceInstance ||
		r.resourceID != editTestInstanceID || r.userID != "owner" {
		t.Errorf("row = %+v", r)
	}
	if r.details["via"] != models.AuditViaUI || r.details["name"] != "vw" {
		t.Errorf("details = %v", r.details)
	}
}

func TestServiceHandler_Delete_RefusedWritesNothing(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.Delete(rr, editRequest(http.MethodDelete, "intruder", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("audit rows = %d, want 0", n)
	}
}

// A config edit records its row before the background recreate starts, with
// an id the recreate can link its outcome to, and never the submitted values.
func TestServiceHandler_UpdateConfig_WritesAuditRowWithoutValues(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	form := url.Values{"name": {"vw"}, "domain": {"vw.example.com"}, "signups_allowed": {"false"}, "admin_token": {"new-secret-token"}}
	rr := httptest.NewRecorder()
	h.UpdateConfig(rr, editRequest(http.MethodPost, "owner", form))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rr.Code)
	}
	rows := rec.snapshot()
	if len(rows) != 1 || rows[0].action != models.AuditActionServiceUpdateConfig || rows[0].id == "" {
		t.Fatalf("rows = %+v", rows)
	}
	if s := fmt.Sprint(rows[0].details); strings.Contains(s, "new-secret-token") || strings.Contains(s, "keep-me") {
		t.Fatalf("audit details leaked a config value: %s", s)
	}
}

// A refused config edit (busy instance) gets a linked .failed row.
func TestServiceHandler_UpdateConfig_BusyWritesFailedRow(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusPending)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.UpdateConfig(rr, editRequest(http.MethodPost, "owner", url.Values{"name": {"vw"}}))
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d", rr.Code)
	}
	rows := rec.snapshot()
	if len(rows) != 2 || rows[1].action != models.AuditActionServiceUpdateConfig+".failed" || rows[1].details["audit_id"] != rows[0].id {
		t.Fatalf("rows = %+v", rows)
	}
}
