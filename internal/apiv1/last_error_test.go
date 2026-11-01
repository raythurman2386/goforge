package apiv1

import (
	"net/http"
	"testing"
)

const (
	failedSvcID        = "abababab-abab-abab-abab-abababababab"
	failedSvcLastError = "image pull failed for example/pg:99: manifest unknown"
)

// GET /services/{id} reports why a failed service failed. The MCP
// get_service tool returns this body as is.
func TestGetServiceShowsLastError(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodGet, "/services/"+failedSvcID, "tok-read", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	svc, _ := out["service"].(map[string]any)
	if svc["status"] != "failed" || svc["last_error"] != failedSvcLastError {
		t.Errorf("service = %v, want status failed and last_error %q", svc, failedSvcLastError)
	}

	// A healthy service has no last_error key at all.
	_, out = h.do(t, http.MethodGet, "/services/"+svcID, "tok-read", "")
	if svc, _ := out["service"].(map[string]any); svc == nil || svc["last_error"] != nil {
		t.Errorf("healthy service = %v, want no last_error", svc)
	}
}
