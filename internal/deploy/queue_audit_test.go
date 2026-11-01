package deploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// memQueue is an in-memory deployment_queue for worker tests.
type memQueue struct {
	mu    sync.Mutex
	items []*models.QueueItem
	done  map[string]string // queue id -> "completed" or failure message
}

func (q *memQueue) Enqueue(_ context.Context, item *models.QueueItem) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item.ID = "q-" + item.DeploymentID
	item.Status = models.QueueStatusPending
	q.items = append(q.items, item)
	return nil
}

func (q *memQueue) Dequeue(context.Context) (*models.QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, nil
	}
	it := q.items[0]
	q.items = q.items[1:]
	return it, nil
}

func (q *memQueue) Complete(_ context.Context, id string) error { return q.finish(id, "completed") }

func (q *memQueue) Fail(_ context.Context, id, msg string) error { return q.finish(id, msg) }

func (q *memQueue) finish(id, v string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.done == nil {
		q.done = map[string]string{}
	}
	q.done[id] = v
	return nil
}

func (q *memQueue) RequeueOrphans(context.Context, time.Duration) (int, error) { return 0, nil }

func (q *memQueue) DeletePendingByDeploymentID(context.Context, string) error { return nil }

// memAudit is an in-memory audit_logs table.
type memAudit struct {
	mu   sync.Mutex
	rows []*models.AuditLog
}

func (a *memAudit) Create(_ context.Context, l *models.AuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = append(a.rows, l)
	return nil
}

func (a *memAudit) GetByID(_ context.Context, id string) (*models.AuditLog, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}

func (a *memAudit) byAction(action string) []*models.AuditLog {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*models.AuditLog
	for _, r := range a.rows {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

const mcpOriginID = "6f1c2b9e-3d4a-4e5f-8a7b-1c2d3e4f5a6b"

// mcpOrigin is the row the API gate writes when an MCP client triggers a
// deployment.
func mcpOrigin(t *testing.T) *models.AuditLog {
	t.Helper()
	details, err := json.Marshal(map[string]any{
		"via": "api_v1", "approval_ref": "t1638u-test", "token_id": "tok-1", "token_name": "ci-bot",
		"target": map[string]any{"project_id": "proj-1", "environment_id": "env-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &models.AuditLog{
		ID:        mcpOriginID,
		UserID:    sql.NullString{String: "11111111-2222-3333-4444-555555555555", Valid: true},
		Action:    "api_v1.trigger_deployment",
		UserAgent: sql.NullString{String: "goforge-mcp-http (test)", Valid: true},
		Details:   details,
		CreatedAt: time.Now(),
	}
}

func runQueued(ctx context.Context, t *testing.T, svc *Service, audit *memAudit) *memQueue {
	t.Helper()
	q := &memQueue{}
	w := &Worker{service: svc, queueRepo: q, timeout: 10 * time.Second}
	w.WithAudit(audit)
	if err := w.Enqueue(ctx, "dep-1"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	item, _ := q.Dequeue(ctx)
	w.processItem(item) //nolint:contextcheck // processItem runs on its own context, as in the worker loop.
	return q
}

// A failed MCP-triggered deployment writes deployment.run.failed carrying
// the original audit_id, token, approval_ref, and user agent.
func TestWorker_FailedMCPDeployWritesLinkedFailedRow(t *testing.T) {
	_, _, _, _, svc := pipelineFixture(t)
	svc.dockerClient = &fakeRuntime{buildErr: errors.New("dockerfile syntax")}
	audit := &memAudit{rows: []*models.AuditLog{mcpOrigin(t)}}

	q := runQueued(models.WithAuditID(context.Background(), mcpOriginID), t, svc, audit)

	rows := audit.byAction("deployment.run.failed")
	if len(rows) != 1 {
		t.Fatalf("deployment.run.failed rows = %d, want 1 (all: %d)", len(rows), len(audit.rows))
	}
	row := rows[0]
	if !row.Failed() {
		t.Error("row.Failed() = false")
	}
	d, err := row.GetDetails()
	if err != nil {
		t.Fatal(err)
	}
	if d["audit_id"] != mcpOriginID {
		t.Errorf("details.audit_id = %v, want %s", d["audit_id"], mcpOriginID)
	}
	for k, want := range map[string]string{
		"origin_action": "api_v1.trigger_deployment", "token_name": "ci-bot", "approval_ref": "t1638u-test",
		"via": "api_v1", "project_id": "proj-1", "status": "failed",
	} {
		if d[k] != want {
			t.Errorf("details.%s = %v, want %s", k, d[k], want)
		}
	}
	if msg, _ := d["error"].(string); !strings.Contains(msg, "dockerfile syntax") {
		t.Errorf("details.error = %q, want the build error", msg)
	}
	if row.UserAgent.String != "goforge-mcp-http (test)" || row.Source() != models.AuditSourceMCP {
		t.Errorf("user agent = %q source = %q, want the MCP origin's", row.UserAgent.String, row.Source())
	}
	if row.UserID.String != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("user_id = %q, want the origin's", row.UserID.String)
	}
	if got := audit.byAction("deployment.run.completed"); len(got) != 0 {
		t.Errorf("completed rows = %d, want 0", len(got))
	}
	if q.done["q-dep-1"] == "completed" {
		t.Error("queue item completed, want failed")
	}
}

func TestWorker_SucceededDeployWritesLinkedCompletedRow(t *testing.T) {
	_, _, _, _, svc := pipelineFixture(t)
	audit := &memAudit{rows: []*models.AuditLog{mcpOrigin(t)}}

	runQueued(models.WithAuditID(context.Background(), mcpOriginID), t, svc, audit)

	rows := audit.byAction("deployment.run.completed")
	if len(rows) != 1 {
		t.Fatalf("deployment.run.completed rows = %d, want 1", len(rows))
	}
	d, _ := rows[0].GetDetails()
	if d["audit_id"] != mcpOriginID || d["status"] != "running" {
		t.Errorf("details = %v", d)
	}
}

// Deployments queued without an audit id (webhooks) write no outcome row.
func TestWorker_NoAuditIDWritesNothing(t *testing.T) {
	_, _, _, _, svc := pipelineFixture(t)
	svc.dockerClient = &fakeRuntime{buildErr: errors.New("boom")}
	audit := &memAudit{}

	runQueued(context.Background(), t, svc, audit)

	if len(audit.rows) != 0 {
		t.Fatalf("audit rows = %d, want 0", len(audit.rows))
	}
}
