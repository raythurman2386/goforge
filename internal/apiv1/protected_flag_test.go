package apiv1

import (
	"net/http"
	"testing"
)

// A service marked protected in the UI (stored flag, not in
// GOFORGE_PROTECTED_SERVICES) is refused like an env-listed one: stop,
// image update, and config edit answer 403 protected_resource with only the
// write scope, before any Docker call or audit row. Restart stays allowed,
// and the protected scope still goes through.
func TestUIProtectedFlagRefusesStop(t *testing.T) {
	h := newHarness(t)
	realRun := `{"dry_run":false,"approval_ref":"t1663u"}`
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/containers/" + uiProtDock + "/stop", realRun},
		{http.MethodPost, "/containers/" + uiProtDock + "/stop", ""}, // dry run too
		{http.MethodPost, "/services/" + uiProtSvcID + "/image", realRun},
		{http.MethodPatch, "/services/" + uiProtSvcID + "/config", `{"config":{"size":"large"},"dry_run":false,"approval_ref":"t1663u"}`},
	}
	for _, tok := range []string{"tok-write", "tok-full"} {
		for _, c := range cases {
			rec, out := h.do(t, c.method, c.path, tok, c.body)
			if rec.Code != http.StatusForbidden || errCode(out) != codeProtected {
				t.Errorf("%s %s %s: got %d %v, want 403 %s", tok, c.method, c.path, rec.Code, out, codeProtected)
			}
		}
	}
	if len(h.docker.stopped)+len(h.instances.imaged)+len(h.instances.updated)+len(h.audit.snapshot()) != 0 {
		t.Fatal("refusal reached docker, the instance service, or the audit log")
	}

	rec, out := h.do(t, http.MethodPost, "/containers/"+uiProtDock+"/restart", "tok-write", realRun)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart on a UI-protected service: %d %v", rec.Code, out)
	}
	rec, out = h.do(t, http.MethodPost, "/containers/"+uiProtDock+"/stop", "tok-protected", realRun)
	if rec.Code != http.StatusOK || len(h.docker.stopped) != 1 {
		t.Fatalf("protected scope stop: %d %v (stopped %v)", rec.Code, out, h.docker.stopped)
	}
}

// Reads report the stored flag: get_service and list_services say
// protected=true for the UI-protected service.
func TestUIProtectedFlagInReads(t *testing.T) {
	h := newHarness(t)
	_, out := h.do(t, http.MethodGet, "/services/"+uiProtSvcID, "tok-read", "")
	svc, _ := out["service"].(map[string]any)
	if svc["protected"] != true {
		t.Errorf("get_service protected = %v", svc["protected"])
	}
	_, out = h.do(t, http.MethodGet, "/services", "tok-read", "")
	list, _ := out["services"].([]any)
	got := map[string]any{}
	for _, it := range list {
		m, _ := it.(map[string]any)
		got[m["id"].(string)] = m["protected"]
	}
	if got[uiProtSvcID] != true || got[protSvcID] != true || got[svcID] != false {
		t.Errorf("list_services protected = %v", got)
	}
}

// If the service behind a container cannot be read, the stop is refused as
// protected rather than let through.
func TestProtectedCheckFailsClosed(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodPost, "/containers/"+dbErrSvcDck+"/stop", "tok-write", `{"dry_run":false,"approval_ref":"t1663u"}`)
	if rec.Code != http.StatusForbidden || errCode(out) != codeProtected {
		t.Fatalf("got %d %v, want 403 %s", rec.Code, out, codeProtected)
	}
	if len(h.docker.stopped) != 0 {
		t.Fatal("stop ran although the protected check could not read the service")
	}
	// The dry run of a container stop reports protected=true in its target.
	_, out = h.do(t, http.MethodPost, "/containers/"+uiProtDock+"/stop", "tok-protected", "")
	target, _ := out["target"].(map[string]any)
	if out["dry_run"] != true || target["protected"] != true {
		t.Errorf("dry run target = %v", out)
	}
}
