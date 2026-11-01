package apiv1

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// A queued deployment answers with an operation the MCP wait_for tool can
// follow; the operation is also kept on the .completed audit row.
func TestTriggerDeploymentReturnsOperation(t *testing.T) {
	h, deps := newRunHarness(t, &slowRollback{}, time.Minute)
	rec, out := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write",
		`{"dry_run":false,"approval_ref":"t1632u-op"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	op, _ := out["operation"].(map[string]any)
	res, _ := out["result"].(map[string]any)
	if op["kind"] != "deployment" || op["id"] == "" || op["id"] != res["id"] || op["project_id"] != projectID || op["poll_tool"] != "wait_for" {
		t.Fatalf("operation = %v, result = %v", op, res)
	}
	if deps.count() != 1 {
		t.Errorf("deployments created = %d", deps.count())
	}
	done := waitForAction(t, h.audit, "api_v1.trigger_deployment.completed")
	if !strings.Contains(string(done.Details), `"poll_tool":"wait_for"`) {
		t.Errorf("completed row lacks the operation: %s", done.Details)
	}

	// A dry run starts nothing and has no operation.
	_, out = h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write", `{}`)
	if out["operation"] != nil || deps.count() != 1 {
		t.Errorf("dry run answered %v", out)
	}
}
