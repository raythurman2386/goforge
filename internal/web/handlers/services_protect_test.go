package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func protectForm(on bool) url.Values {
	if on {
		return url.Values{"protected": {"true", "false"}}
	}
	return url.Values{"protected": {"false"}}
}

// Turning the toggle on stores the flag, writes one service.set_protected
// row (via=ui), and re-renders the card checked. Turning it off does the
// reverse. Posting the current value writes nothing.
func TestServiceHandler_SetProtected_TogglesAndAudits(t *testing.T) {
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.SetProtected(rr, editRequest(http.MethodPost, "owner", protectForm(true)))
	if rr.Code != http.StatusOK {
		t.Fatalf("on: status = %d body=%s", rr.Code, rr.Body)
	}
	if !repo.inst.Protected {
		t.Fatal("flag not stored")
	}
	if body := rr.Body.String(); !strings.Contains(body, `id="service-protected"`) || !strings.Contains(body, " checked") {
		t.Fatalf("card not re-rendered checked: %s", body)
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.action != models.AuditActionServiceSetProtected || r.resourceType != models.ResourceTypeServiceInstance ||
		r.resourceID != editTestInstanceID || r.userID != "owner" {
		t.Errorf("row = %+v", r)
	}
	if r.details["via"] != models.AuditViaUI || r.details["protected"] != true || r.details["previous"] != false || r.details["name"] != "vw" {
		t.Errorf("details = %v", r.details)
	}

	// Same value again: no change, no row.
	rr = httptest.NewRecorder()
	h.SetProtected(rr, editRequest(http.MethodPost, "owner", protectForm(true)))
	if rr.Code != http.StatusOK || len(rec.snapshot()) != 1 {
		t.Fatalf("no-op: status=%d rows=%d", rr.Code, len(rec.snapshot()))
	}

	rr = httptest.NewRecorder()
	h.SetProtected(rr, editRequest(http.MethodPost, "owner", protectForm(false)))
	if rr.Code != http.StatusOK || repo.inst.Protected {
		t.Fatalf("off: status=%d protected=%v", rr.Code, repo.inst.Protected)
	}
	rows = rec.snapshot()
	if len(rows) != 2 || rows[1].details["protected"] != false || rows[1].details["previous"] != true {
		t.Fatalf("off rows = %+v", rows)
	}
}

func TestServiceHandler_SetProtected_NonOwnerRefused(t *testing.T) {
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.SetProtected(rr, editRequest(http.MethodPost, "intruder", protectForm(true)))
	if rr.Code != http.StatusForbidden || repo.inst.Protected || len(rec.snapshot()) != 0 {
		t.Fatalf("status=%d protected=%v rows=%d", rr.Code, repo.inst.Protected, len(rec.snapshot()))
	}
}

// A service in GOFORGE_PROTECTED_SERVICES cannot be turned off from the UI.
func TestServiceHandler_SetProtected_EnvListedCannotTurnOff(t *testing.T) {
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	h.WithEnvProtected([]string{" " + editTestInstanceID + " "})
	rec := &fakeUIAudit{}
	h.WithAudit(rec)

	rr := httptest.NewRecorder()
	h.SetProtected(rr, editRequest(http.MethodPost, "owner", protectForm(false)))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "GOFORGE_PROTECTED_SERVICES") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body)
	}
	if repo.inst.Protected || len(rec.snapshot()) != 0 {
		t.Fatal("refused toggle changed state or wrote audit")
	}
	if p := h.protection(&repo.inst); !p.On() || !p.Env {
		t.Fatalf("protection = %+v", p)
	}
}

// Delete requires the exact service name, from the query (htmx DELETE) or
// a form body. Anything else is 400 with no delete and no audit row.
func TestServiceHandler_Delete_RequiresTypedName(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"missing", nil},
		{"wrong", url.Values{"confirm_name": {"VW"}}},
		{"empty", url.Values{"confirm_name": {"  "}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newEditHandler(t, models.ServiceStatusRunning)
			rec := &fakeUIAudit{}
			h.WithAudit(rec)
			rr := httptest.NewRecorder()
			h.Delete(rr, editRequest(http.MethodDelete, "owner", tc.form))
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "vw") {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body)
			}
			if n := len(rec.snapshot()); n != 0 {
				t.Fatalf("audit rows = %d", n)
			}
		})
	}

	t.Run("query with spaces", func(t *testing.T) {
		h, _ := newEditHandler(t, models.ServiceStatusRunning)
		rec := &fakeUIAudit{}
		h.WithAudit(rec)
		req := editRequest(http.MethodDelete, "owner", nil)
		req.URL.RawQuery = url.Values{"confirm_name": {" vw "}}.Encode()
		rr := httptest.NewRecorder()
		h.Delete(rr, req)
		if rr.Code != http.StatusOK || rr.Header().Get("HX-Redirect") != "/services" || len(rec.snapshot()) != 1 {
			t.Fatalf("status=%d redirect=%q rows=%d", rr.Code, rr.Header().Get("HX-Redirect"), len(rec.snapshot()))
		}
	})
}
