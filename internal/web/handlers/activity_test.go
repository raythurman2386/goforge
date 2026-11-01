package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

type fakeAuditLister struct {
	logs []*models.AuditLog
	err  error
	got  []repositories.AuditFilter
}

func (f *fakeAuditLister) ListFiltered(_ context.Context, filter repositories.AuditFilter) ([]*models.AuditLog, error) {
	f.got = append(f.got, filter)
	if f.err != nil {
		return nil, f.err
	}
	return f.logs, nil
}

func auditRow(t *testing.T, id, action, rtype, rid, ua string, details map[string]any) *models.AuditLog {
	t.Helper()
	b, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	return &models.AuditLog{
		ID:           id,
		UserID:       sql.NullString{String: "user-1", Valid: true},
		Action:       action,
		ResourceType: rtype,
		ResourceID:   sql.NullString{String: rid, Valid: rid != ""},
		Details:      b,
		UserAgent:    sql.NullString{String: ua, Valid: ua != ""},
		CreatedAt:    time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
	}
}

func activityGet(t *testing.T, h http.HandlerFunc, target string, user *models.User) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user))
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

const (
	restartAuditID = "11111111-1111-4111-8111-111111111111"
	restartSvcID   = "22222222-2222-4222-8222-222222222222"
)

// The done-means row: an MCP agent restarted a service through
// api_v1.restart_service with the ci-bot token and an approval.
func TestActivityPage_RendersAPIRestartRowWithTokenName(t *testing.T) {
	store := &fakeAuditLister{logs: []*models.AuditLog{
		auditRow(t, restartAuditID, "api_v1.restart_service", models.ResourceTypeServiceInstance, restartSvcID,
			"goforge-mcp/1.0", map[string]any{
				"via": "api_v1", "token_name": "ci-bot", "token_id": "tok-1",
				"approval_ref": "tater:t1638s6668",
			}),
	}}
	h := NewActivityHandler(store, slog.Default())
	w := activityGet(t, h.Page, "/settings/activity", &models.User{ID: "user-1", Role: models.RoleUser})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	html := w.Body.String()
	for _, want := range []string{
		"api_v1.restart_service",
		">ci-bot<",
		"tater:t1638s6668",
		`href="/services/` + restartSvcID + `"`,
		">MCP<",
		`data-activity-id="` + restartAuditID + `"`,
		`href="/settings/activity?audit_id=` + restartAuditID + `"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("activity page missing %q", want)
		}
	}
	if strings.Contains(html, `data-failed="true"`) {
		t.Error("successful row rendered as failed")
	}
	if len(store.got) != 1 || store.got[0].UserID != "user-1" || store.got[0].Limit != activityPageSize+1 {
		t.Errorf("filter = %+v, want own rows, limit %d", store.got, activityPageSize+1)
	}
}

func TestActivityPage_FailedOutcomeHighlightedAndLinkedToOrigin(t *testing.T) {
	store := &fakeAuditLister{logs: []*models.AuditLog{
		auditRow(t, "33333333-3333-4333-8333-333333333333", "deployment.run.failed", models.ResourceTypeDeployment,
			"44444444-4444-4444-8444-444444444444", "goforge-mcp/1.0", map[string]any{
				"audit_id": restartAuditID, "token_name": "ci-bot", "project_id": "proj-1",
				"error": "build failed: exit 1",
			}),
	}}
	h := NewActivityHandler(store, slog.Default())
	html := activityGet(t, h.Page, "/settings/activity", &models.User{ID: "user-1"}).Body.String()
	for _, want := range []string{
		`data-failed="true"`, "bg-red-50/70", "build failed: exit 1",
		`href="/projects/proj-1/deployments/44444444-4444-4444-8444-444444444444"`,
		`href="/settings/activity?audit_id=` + restartAuditID + `"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("activity page missing %q", want)
		}
	}
}

func TestActivityPage_Filters(t *testing.T) {
	store := &fakeAuditLister{}
	h := NewActivityHandler(store, slog.Default())
	member := &models.User{ID: "user-1", Role: models.RoleUser}
	admin := &models.User{ID: "admin-1", Role: models.RoleAdmin}

	activityGet(t, h.Page, "/settings/activity?source=mcp&token=ci-bot&action=restart&audit_id=x&all=1", member)
	activityGet(t, h.Page, "/settings/activity?source=bogus&all=1&offset=50", admin)

	if len(store.got) != 2 {
		t.Fatalf("calls = %d", len(store.got))
	}
	m := store.got[0]
	if m.UserID != "user-1" || m.Source != models.AuditSourceMCP || m.TokenName != "ci-bot" || m.ActionContains != "restart" || m.AuditID != "x" {
		t.Errorf("member filter = %+v (all=1 must not widen a non-admin)", m)
	}
	a := store.got[1]
	if a.UserID != "" || a.Source != "" || a.Offset != 50 {
		t.Errorf("admin filter = %+v, want all users, unknown source dropped, offset 50", a)
	}
}

func TestActivityPage_PagesOlder(t *testing.T) {
	logs := make([]*models.AuditLog, activityPageSize+1)
	for i := range logs {
		logs[i] = auditRow(t, "id", "project.update", models.ResourceTypeProject, "p", "", nil)
	}
	h := NewActivityHandler(&fakeAuditLister{logs: logs}, slog.Default())
	html := activityGet(t, h.Page, "/settings/activity?source=ui", &models.User{ID: "u"}).Body.String()
	if !strings.Contains(html, `href="/settings/activity?offset=50&amp;source=ui"`) {
		t.Error("missing Older link carrying the filter")
	}
	if got := strings.Count(html, "data-activity-id="); got != activityPageSize {
		t.Errorf("rows = %d, want %d", got, activityPageSize)
	}
}

func TestActivityPage_AuthAndErrors(t *testing.T) {
	h := NewActivityHandler(&fakeAuditLister{err: errors.New("db down")}, slog.Default())
	if w := activityGet(t, h.Page, "/settings/activity", nil); w.Code != http.StatusFound {
		t.Errorf("anonymous page status %d, want redirect", w.Code)
	}
	if w := activityGet(t, h.Recent, "/dashboard/activity", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous card status %d", w.Code)
	}
	w := activityGet(t, h.Page, "/settings/activity", &models.User{ID: "u"})
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "db down") {
		t.Errorf("error status %d body %q", w.Code, w.Body.String())
	}
}

func TestActivityRecent_ShowsOwnAgentActions(t *testing.T) {
	store := &fakeAuditLister{logs: []*models.AuditLog{
		auditRow(t, restartAuditID, "api_v1.restart_service", models.ResourceTypeServiceInstance, restartSvcID,
			"curl/8", map[string]any{"via": "api_v1", "token_name": "ci-bot"}),
	}}
	h := NewActivityHandler(store, slog.Default())
	html := activityGet(t, h.Recent, "/dashboard/activity", &models.User{ID: "user-1"}).Body.String()
	for _, want := range []string{`id="recent-agent-actions"`, "Recent agent actions", "api_v1.restart_service", ">ci-bot<", ">API<", `/settings/activity?source=agent`} {
		if !strings.Contains(html, want) {
			t.Errorf("card missing %q", want)
		}
	}
	f := store.got[0]
	if f.UserID != "user-1" || f.Source != models.AuditSourceAgent || f.Limit != activityRecentSize {
		t.Errorf("card filter = %+v", f)
	}
}
