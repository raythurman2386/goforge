package apiv1

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
)

func countActions(a *fakeAudit, action string) int {
	n := 0
	for _, row := range a.snapshot() {
		if row.Action == action {
			n++
		}
	}
	return n
}

func errDetail(out map[string]any) map[string]any {
	e, _ := out["error"].(map[string]any)
	return e
}

// trigger_deployment twice with the same approval_ref makes one deployment
// row. The retry answers 200 with the first result, operation, and audit_id.
func TestTriggerDeploymentTwiceSameRefMakesOneDeployment(t *testing.T) {
	h, deps := newRunHarness(t, &slowRollback{}, time.Minute)
	body := `{"dry_run":false,"approval_ref":"t1632u-idem"}`
	rec1, first := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write", body)
	rec2, second := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write", body)
	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Fatalf("got %d %s then %d %s", rec1.Code, rec1.Body, rec2.Code, rec2.Body)
	}
	if deps.count() != 1 {
		t.Fatalf("deployments created = %d, want 1", deps.count())
	}
	if first["audit_id"] == "" || second["audit_id"] != first["audit_id"] {
		t.Errorf("audit_id %v then %v", first["audit_id"], second["audit_id"])
	}
	r1, _ := first["result"].(map[string]any)
	r2, _ := second["result"].(map[string]any)
	if r1["id"] == nil || r2["id"] != r1["id"] {
		t.Errorf("result id %v then %v", r1["id"], r2["id"])
	}
	op, _ := second["operation"].(map[string]any)
	if op["id"] != r1["id"] || op["poll_tool"] != "wait_for" {
		t.Errorf("replayed operation %v", op)
	}
	if first["replayed"] != nil || second["replayed"] != true {
		t.Errorf("replayed %v then %v", first["replayed"], second["replayed"])
	}
	if n := countActions(h.audit, "api_v1.trigger_deployment"); n != 1 {
		t.Errorf("approval rows = %d, want 1", n)
	}

	// A new approval_ref is a new deployment.
	rec3, third := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write",
		`{"dry_run":false,"approval_ref":"t1632u-idem-2"}`)
	if rec3.Code != http.StatusOK || deps.count() != 2 || third["audit_id"] == first["audit_id"] {
		t.Errorf("new ref: %d %v, deployments = %d", rec3.Code, third, deps.count())
	}
}

type countingRollback struct {
	slowRollback
	calls atomic.Int32
}

func (c *countingRollback) Rollback(ctx context.Context, a, b string) (*deploy.RollbackInfo, error) {
	c.calls.Add(1)
	return c.slowRollback.Rollback(ctx, a, b)
}

// A retry while the first run is still going answers 409 in_progress with
// the first audit_id and does not start a second rollback; once it ends, a
// retry gets the first result.
func TestRetryWhileRunningIsInProgress(t *testing.T) {
	rb := &countingRollback{slowRollback: slowRollback{delay: 300 * time.Millisecond}}
	h, _ := newRunHarness(t, rb, 30*time.Millisecond)
	path := "/projects/" + projectID + "/deployments/" + deplID + "/rollback"
	body := `{"dry_run":false,"approval_ref":"t1632u-idem-rb"}`

	rec, out := h.do(t, http.MethodPost, path, "tok-write", body)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	auditID := errDetail(out)["audit_id"]

	rec, out = h.do(t, http.MethodPost, path, "tok-write", body)
	if rec.Code != http.StatusConflict || errCode(out) != codeInProgress || errDetail(out)["audit_id"] != auditID {
		t.Fatalf("retry while running: %d %v", rec.Code, out)
	}

	waitForAction(t, h.audit, "api_v1.rollback_deployment.completed")
	rec, out = h.do(t, http.MethodPost, path, "tok-write", body)
	if rec.Code != http.StatusOK || out["audit_id"] != auditID || out["replayed"] != true {
		t.Fatalf("retry after finish: %d %v", rec.Code, out)
	}
	if n := rb.calls.Load(); n != 1 {
		t.Errorf("rollback ran %d times, want 1", n)
	}
}

type countingFailRollback struct {
	slowRollback
	calls atomic.Int32
}

func (c *countingFailRollback) Rollback(context.Context, string, string) (*deploy.RollbackInfo, error) {
	c.calls.Add(1)
	return nil, context.DeadlineExceeded
}

// A retry of a failed run returns the stored error and audit_id and does not
// run again.
func TestRetryAfterFailureReturnsFirstError(t *testing.T) {
	rb := &countingFailRollback{}
	h, _ := newRunHarness(t, rb, time.Minute)
	path := "/projects/" + projectID + "/deployments/" + deplID + "/rollback"
	body := `{"dry_run":false,"approval_ref":"t1632u-idem-fail"}`

	rec1, first := h.do(t, http.MethodPost, path, "tok-write", body)
	if rec1.Code < 400 {
		t.Fatalf("first: %d %s", rec1.Code, rec1.Body)
	}
	rec2, second := h.do(t, http.MethodPost, path, "tok-write", body)
	if rec2.Code != rec1.Code || errCode(second) != errCode(first) || errDetail(second)["replayed"] != true {
		t.Fatalf("retry: %d %v, first %d %v", rec2.Code, second, rec1.Code, first)
	}
	if errDetail(second)["audit_id"] == nil || errDetail(second)["audit_id"] == "" {
		t.Errorf("retry has no audit_id: %v", second)
	}
	if n := rb.calls.Load(); n != 1 {
		t.Errorf("rollback ran %d times, want 1", n)
	}
}

// racyAudit misses the first lookup, as when two requests with the same
// approval_ref check at the same moment; the unique index then refuses the
// second insert and the request answers as the first run.
type racyAudit struct {
	*fakeAudit
	missed atomic.Bool
}

func (r *racyAudit) FindApproval(ctx context.Context, userID, action, ref, key string) (*models.AuditLog, error) {
	if len(r.snapshot()) > 0 && r.missed.CompareAndSwap(false, true) {
		return nil, nil
	}
	return r.fakeAudit.FindApproval(ctx, userID, action, ref, key)
}

func TestConcurrentSameRefLosesInsertAndReplays(t *testing.T) {
	h, deps := newRunHarnessWith(t, &slowRollback{}, time.Minute, func(a *fakeAudit) AuditStore { return &racyAudit{fakeAudit: a} })
	body := `{"dry_run":false,"approval_ref":"t1632u-idem-race"}`
	_, first := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write", body)
	rec, second := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments", "tok-write", body)
	if rec.Code != http.StatusOK || second["replayed"] != true || second["audit_id"] != first["audit_id"] {
		t.Fatalf("second: %d %v", rec.Code, second)
	}
	if deps.count() != 1 {
		t.Errorf("deployments created = %d, want 1", deps.count())
	}
}

// One approval_ref may cover changes to different variables in the same
// environment; a deployment status change does not alter the key.
func TestIdempotencyKeyTarget(t *testing.T) {
	setA := &mutation{Action: "set_env_var", ResourceType: "environment", ResourceID: "e1", Target: map[string]any{"key": "A"}}
	setB := &mutation{Action: "set_env_var", ResourceType: "environment", ResourceID: "e1", Target: map[string]any{"key": "B"}}
	if idempotencyKey(setA) == idempotencyKey(setB) {
		t.Error("different keys share an idempotency key")
	}
	c1 := &mutation{ResourceType: "deployment", ResourceID: "d1", Target: map[string]any{"status": "queued"}}
	c2 := &mutation{ResourceType: "deployment", ResourceID: "d1", Target: map[string]any{"status": "cancelled"}}
	if idempotencyKey(c1) != idempotencyKey(c2) {
		t.Error("status change altered the idempotency key")
	}
	n1 := &mutation{ResourceType: "service_instance", Target: map[string]any{"template": "redis", "name": "a"}}
	n2 := &mutation{ResourceType: "service_instance", Target: map[string]any{"template": "redis", "name": "b"}}
	if idempotencyKey(n1) == idempotencyKey(n2) {
		t.Error("two new services share an idempotency key")
	}
}
