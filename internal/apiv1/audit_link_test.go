package apiv1

import (
	"net/http"
	"testing"
)

// A queued change gets the gate's audit id on its context, so the service
// or deploy worker can link its final outcome row to it, and the gate's own
// outcome row names the token like the request row does.
func TestRunPassesAuditIDAndTokenToOutcome(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodPost, "/services/"+svcID+"/image", "tok-write", `{"dry_run":false,"approval_ref":"t1638u"}`)
	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("update_service_image: %d %v", rec.Code, out)
	}
	auditID, _ := out["audit_id"].(string)
	if auditID == "" {
		t.Fatalf("no audit_id in %v", out)
	}
	if len(h.instances.imageAuditIDs) != 1 || h.instances.imageAuditIDs[0] != auditID {
		t.Fatalf("op context audit ids = %v, want [%s]", h.instances.imageAuditIDs, auditID)
	}
	var outcome map[string]any
	for _, row := range h.audit.snapshot() {
		if row.Action == "api_v1.update_service_image.completed" {
			outcome, _ = row.GetDetails()
		}
	}
	if outcome == nil {
		t.Fatal("no api_v1.update_service_image.completed row")
	}
	if outcome["audit_id"] != auditID || outcome["token_name"] != "tok-write" || outcome["token_id"] != "id-tok-write" {
		t.Fatalf("outcome details = %v", outcome)
	}
}
