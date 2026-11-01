package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/middleware"
)

const (
	deplID   = "99999999-9999-9999-9999-999999999999"
	prevDepl = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

type fakeDeployments struct {
	Deployments
	mu      sync.Mutex
	created []*models.Deployment
	byID    map[string]*models.Deployment
	logs    map[string][]*models.DeploymentLog
}

func (f *fakeDeployments) GetDeployment(_ context.Context, id string) (*models.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == deplID {
		return &models.Deployment{ID: deplID, EnvironmentID: envID, Status: models.DeploymentStatusRunning}, nil
	}
	if d, ok := f.byID[id]; ok {
		cp := *d
		return &cp, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeDeployments) CreateDeployment(_ context.Context, d *models.Deployment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.ID = "dep-" + string(rune('a'+len(f.created)))
	f.created = append(f.created, d)
	if f.byID == nil {
		f.byID = map[string]*models.Deployment{}
	}
	f.byID[d.ID] = d
	return nil
}

func (f *fakeDeployments) GetLogs(_ context.Context, id string) ([]*models.DeploymentLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logs[id], nil
}

func (f *fakeDeployments) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

type fakeQueue struct{ DeployQueue }

func (fakeQueue) Enqueue(context.Context, string) error { return nil }

// slowRollback stands in for a rollback that stops the current containers,
// then takes longer than the request may wait before it starts the target.
type slowRollback struct {
	delay   time.Duration
	mu      sync.Mutex
	stopped bool
	started bool
	ctxErr  error
}

func (s *slowRollback) CanRollback(*models.Deployment) bool { return true }

func (s *slowRollback) Rollback(ctx context.Context, _, _ string) (*deploy.RollbackInfo, error) {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		s.mu.Lock()
		s.ctxErr = ctx.Err()
		s.mu.Unlock()
		return nil, ctx.Err()
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	return &deploy.RollbackInfo{FromDeploymentID: deplID, ToDeploymentID: prevDepl}, nil
}

func (s *slowRollback) state() (stopped, started bool, ctxErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped, s.started, s.ctxErr
}

// newRunHarness is newHarness with deployments, a queue, and rollback, and
// a short wait so tests can push a change past it.
func newRunHarness(t *testing.T, rb Rollbacker, syncWait time.Duration) (*harness, *fakeDeployments) {
	t.Helper()
	return newRunHarnessWith(t, rb, syncWait, nil)
}

// newRunHarnessWith is newRunHarness with audit wrapped by wrap (nil keeps
// the plain fake).
func newRunHarnessWith(t *testing.T, rb Rollbacker, syncWait time.Duration, wrap func(*fakeAudit) AuditStore) (*harness, *fakeDeployments) {
	t.Helper()
	h := newHarness(t)
	var audit AuditStore = h.audit
	if wrap != nil {
		audit = wrap(h.audit)
	}
	tokens := &fakeTokens{byHash: map[string]*models.APIToken{}}
	tokens.byHash[auth.HashAPIToken("tok-write")] = &models.APIToken{ID: "id-tok-write", UserID: ownerID, Name: "tok-write", Scopes: []string{models.ScopeWrite}}
	deps := &fakeDeployments{}
	h.srv = New(Deps{
		Tokens:      tokens,
		Users:       fakeUsers{},
		Projects:    h.projects,
		Instances:   h.instances,
		Containers:  fakeContainers{},
		Docker:      h.docker,
		Audit:       audit,
		Deployments: deps,
		Queue:       fakeQueue{},
		Rollback:    rb,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		SyncWait:    syncWait,
	}).Routes()
	return h, deps
}

func waitForAction(t *testing.T, a *fakeAudit, action string) *models.AuditLog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range a.snapshot() {
			if row.Action == action {
				return row
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s audit row; have %d rows", action, len(a.snapshot()))
	return nil
}

// A rollback that outlives the request wait answers JSON 504 "timeout" with
// the audit_id, keeps running to the end instead of stopping half-applied,
// and writes its .completed row.
func TestSlowRollbackAnswersJSONTimeoutAndFinishes(t *testing.T) {
	rb := &slowRollback{delay: 300 * time.Millisecond}
	h, _ := newRunHarness(t, rb, 50*time.Millisecond)

	rec, out := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments/"+deplID+"/rollback", "tok-write",
		`{"dry_run":false,"approval_ref":"t1632u-rb"}`)
	if rec.Code != http.StatusGatewayTimeout || errCode(out) != codeTimeout {
		t.Fatalf("got %d %s, want 504 timeout", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
	e, _ := out["error"].(map[string]any)
	auditID, _ := e["audit_id"].(string)
	if auditID == "" {
		t.Fatalf("timeout answer has no audit_id: %v", out)
	}

	done := waitForAction(t, h.audit, "api_v1.rollback_deployment.completed")
	var details map[string]any
	_ = json.Unmarshal(done.Details, &details)
	if details["audit_id"] != auditID || details["approval_ref"] != "t1632u-rb" {
		t.Errorf("completed details %v", details)
	}
	if stopped, started, ctxErr := rb.state(); !stopped || !started || ctxErr != nil {
		t.Errorf("rollback stopped=%v started=%v ctxErr=%v; want it to finish", stopped, started, ctxErr)
	}
}

// The same rollback behind the router's JSON timeout middleware: the outer
// deadline is near, so the handler answers its own JSON timeout first, and
// the request context ending does not cancel the change.
func TestRollbackUnderRequestDeadline(t *testing.T) {
	rb := &slowRollback{delay: 400 * time.Millisecond}
	h, _ := newRunHarness(t, rb, time.Minute)
	srv := middleware.TimeoutJSON(200 * time.Millisecond)(h.srv)

	req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID+"/deployments/"+deplID+"/rollback",
		strings.NewReader(`{"dry_run":false,"approval_ref":"t1632u-rb2"}`))
	req.Header.Set("Authorization", "Bearer tok-write")
	req.RemoteAddr = "192.0.2.10:5555"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("non-JSON answer %d %q", rec.Code, rec.Body.String())
	}
	if errCode(out) != codeTimeout {
		t.Fatalf("got %d %v, want code timeout", rec.Code, out)
	}
	waitForAction(t, h.audit, "api_v1.rollback_deployment.completed")
	if _, started, ctxErr := rb.state(); !started || ctxErr != nil {
		t.Errorf("rollback started=%v ctxErr=%v; the request deadline canceled it", started, ctxErr)
	}
}

type failingRollback struct{ slowRollback }

func (f *failingRollback) Rollback(context.Context, string, string) (*deploy.RollbackInfo, error) {
	time.Sleep(100 * time.Millisecond)
	return nil, errors.New("no previous successful deployment")
}

// A change that fails after the caller stopped waiting still writes its
// .failed row, on a context of its own.
func TestFailureAfterTimeoutIsAudited(t *testing.T) {
	h, _ := newRunHarness(t, &failingRollback{}, 20*time.Millisecond)
	rec, out := h.do(t, http.MethodPost, "/projects/"+projectID+"/deployments/"+deplID+"/rollback", "tok-write",
		`{"dry_run":false,"approval_ref":"t1632u-rb3"}`)
	if rec.Code != http.StatusGatewayTimeout || errCode(out) != codeTimeout {
		t.Fatalf("got %d %v", rec.Code, out)
	}
	row := waitForAction(t, h.audit, "api_v1.rollback_deployment.failed")
	if !strings.Contains(string(row.Details), "no previous successful deployment") {
		t.Errorf("failed details %s", row.Details)
	}
}

type panickyInstances struct{ *fakeInstances }

func (panickyInstances) RestartInstance(context.Context, string) error { panic("boom") }

// A panic in a detached change is recovered, answered 500, and audited.
func TestPanicInChangeIsRecovered(t *testing.T) {
	h := newHarness(t)
	tokens := &fakeTokens{byHash: map[string]*models.APIToken{
		auth.HashAPIToken("tok-write"): {ID: "id", UserID: ownerID, Name: "w", Scopes: []string{models.ScopeWrite}},
	}}
	h.srv = New(Deps{
		Tokens: tokens, Users: fakeUsers{}, Projects: h.projects,
		Instances: panickyInstances{h.instances}, Audit: h.audit,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Routes()
	rec, out := h.do(t, http.MethodPost, "/services/"+svcID+"/restart", "tok-write", `{"dry_run":false,"approval_ref":"t1632u-p"}`)
	if rec.Code != http.StatusInternalServerError || errCode(out) != codeInternal {
		t.Fatalf("got %d %v", rec.Code, out)
	}
	waitForAction(t, h.audit, "api_v1.restart_service.failed")
}
