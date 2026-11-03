package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/audit"
	"github.com/raythurman2386/goforge/internal/models"
)

type fakeAuditRetention struct {
	view    *audit.SettingsView
	saveErr error
	old     int
	new     int
	changed bool
	raw     string
}

func (f *fakeAuditRetention) View(context.Context) (*audit.SettingsView, error) {
	if f.view == nil {
		return &audit.SettingsView{}, nil
	}
	return f.view, nil
}

func (f *fakeAuditRetention) Save(_ context.Context, raw, _ string) (int, int, bool, error) {
	f.raw = raw
	return f.old, f.new, f.changed, f.saveErr
}

func auditRetentionHandler(s *fakeAuditRetention, a *fakeAudit) *SettingsHandler {
	return (&SettingsHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).
		WithAuditRetention(s).WithAudit(a)
}

func TestUpdateAuditRetention_NonAdmin(t *testing.T) {
	s, a := &fakeAuditRetention{}, &fakeAudit{}
	h := auditRetentionHandler(s, a)
	for _, tc := range []struct {
		name string
		user *models.User
		want int
	}{{"anonymous", nil, http.StatusUnauthorized}, {"non-admin", plainUser, http.StatusForbidden}} {
		rr := httptest.NewRecorder()
		h.UpdateAuditRetention(rr, backupReq(http.MethodPut, "/settings/audit-retention", url.Values{"retention_days": {"30"}}, tc.user))
		if rr.Code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, rr.Code, tc.want)
		}
	}
	if s.raw != "" || len(a.actions) != 0 {
		t.Fatalf("non-admin reached save raw=%q actions=%v", s.raw, a.actions)
	}
}

func TestUpdateAuditRetention_FieldError(t *testing.T) {
	s := &fakeAuditRetention{saveErr: &audit.FieldError{Field: "retention_days", Message: "must be 0 (keep forever) or between 30 and 3650"}}
	a := &fakeAudit{}
	rr := httptest.NewRecorder()
	auditRetentionHandler(s, a).UpdateAuditRetention(rr, backupReq(http.MethodPut, "/settings/audit-retention", url.Values{"retention_days": {"10"}}, adminUser))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "retention_days") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(a.actions) != 0 {
		t.Fatal("field error was audited")
	}
}

func TestUpdateAuditRetention_AuditsOldAndNew(t *testing.T) {
	s := &fakeAuditRetention{old: 0, new: 30, changed: true, view: &audit.SettingsView{Days: 30, Source: audit.SourceSettings}}
	a := &fakeAudit{}
	rr := httptest.NewRecorder()
	auditRetentionHandler(s, a).UpdateAuditRetention(rr, backupReq(http.MethodPut, "/settings/audit-retention", url.Values{"retention_days": {"30"}}, adminUser))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body)
	}
	if !strings.Contains(rr.Body.String(), `data-testid="audit-retention-card"`) {
		t.Fatalf("card missing: %s", rr.Body.String())
	}
	if len(a.actions) != 1 || a.actions[0] != models.AuditActionAuditSettingsUpdate {
		t.Fatalf("actions = %v", a.actions)
	}
	d := a.details[0]
	if d["old_days"] != 0 || d["new_days"] != 30 {
		t.Fatalf("details = %#v", d)
	}
}

func TestActivityPage_RetentionAndIntegrity(t *testing.T) {
	h := NewActivityHandler(&fakeAuditLister{}, slog.Default()).
		WithRetention(func(context.Context) (int, error) { return 30, nil }).
		WithIntegrity(func() audit.IntegrityView {
			return audit.IntegrityView{Checked: true, OK: true, Seq: 7}
		})
	admin := activityGet(t, h.Page, "/settings/activity", &models.User{ID: "u", Role: models.RoleAdmin})
	body := admin.Body.String()
	if !strings.Contains(body, `data-testid="audit-retention-note"`) || !strings.Contains(body, "Rows older than 30 days are deleted daily.") {
		t.Fatalf("retention note missing: %s", body)
	}
	if !strings.Contains(body, `data-testid="audit-integrity"`) || !strings.Contains(body, "Integrity: verified through #7") {
		t.Fatalf("integrity line missing: %s", body)
	}
	member := activityGet(t, h.Page, "/settings/activity", &models.User{ID: "u", Role: models.RoleUser})
	if strings.Contains(member.Body.String(), `data-testid="audit-integrity"`) {
		t.Fatal("non-admin saw the integrity line")
	}
}
