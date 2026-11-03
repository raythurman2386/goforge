package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/httputil"
)

type fakeProjects struct{ projects []*models.Project }

func (f *fakeProjects) ListByRepoURL(_ context.Context, url string) ([]*models.Project, error) {
	var out []*models.Project
	for _, p := range f.projects {
		if p.RepositoryURL == url {
			out = append(out, p)
		}
	}
	return out, nil
}

type fakeEnvs struct {
	envs map[string][]*models.Environment
}

func (f *fakeEnvs) ListByProjectID(_ context.Context, id string) ([]*models.Environment, error) {
	return f.envs[id], nil
}

type fakeDeploys struct {
	mu      sync.Mutex
	created []*models.Deployment
}

func (f *fakeDeploys) CreateDeployment(_ context.Context, d *models.Deployment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", len(f.created)+1)
	f.created = append(f.created, d)
	return nil
}

type enqueued struct{ deploymentID, auditID string }

type fakeQueue struct {
	mu    sync.Mutex
	items []enqueued
	err   error
}

func (f *fakeQueue) Enqueue(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, enqueued{id, models.AuditIDFrom(ctx)})
	return f.err
}

type auditRow struct {
	id, userID, action, resourceType, resourceID string
	details                                      map[string]interface{}
	ip, userAgent                                string
}

type fakeAudit struct {
	mu   sync.Mutex
	rows []auditRow
}

func (f *fakeAudit) RecordAs(_ context.Context, id, userID, action, rt, rid string, d map[string]interface{}, ip, ua string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, auditRow{id, userID, action, rt, rid, d, ip, ua})
}

const (
	auditTestSecret = "audit-test-secret" //nolint:gosec // HMAC key for a fake push in tests, not a credential
	auditTestRepo   = "https://github.com/acme/app"
	auditTestOwner  = "11111111-1111-4111-8111-111111111111"
	auditTestSHA    = "0123456789abcdef0123456789abcdef01234567"
)

// newAuditHandler wires a project with two environments: production
// (auto-deploy, main) and staging (auto-deploy off).
func newAuditHandler(qErr error) (*Handler, *fakeDeploys, *fakeQueue, *fakeAudit) {
	proj := &models.Project{ID: "22222222-2222-4222-8222-222222222222", UserID: auditTestOwner, Name: "app",
		RepositoryURL: auditTestRepo, DefaultBranch: "main"}
	envs := map[string][]*models.Environment{proj.ID: {
		{ID: "33333333-3333-4333-8333-333333333333", ProjectID: proj.ID, Name: "production", AutoDeploy: true},
		{ID: "44444444-4444-4444-8444-444444444444", ProjectID: proj.ID, Name: "staging", AutoDeploy: false},
	}}
	deploys, queue, audit := &fakeDeploys{}, &fakeQueue{err: qErr}, &fakeAudit{}
	h := &Handler{
		deployService:   deploys,
		projectRepo:     &fakeProjects{projects: []*models.Project{proj}},
		environmentRepo: &fakeEnvs{envs: envs},
		worker:          queue,
		webhookSecret:   auditTestSecret,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	h.WithAudit(audit, httputil.ProxyTrust{})
	return h, deploys, queue, audit
}

func signedGitHubPush(t *testing.T, branch string) *http.Request {
	t.Helper()
	body := []byte(`{"ref":"refs/heads/` + branch + `","after":"` + auditTestSHA + `",` +
		`"head_commit":{"id":"` + auditTestSHA + `","message":"ship it","author":{"name":"dev","email":"dev@example.com"}},` +
		`"repository":{"full_name":"acme/app","html_url":"` + auditTestRepo + `"}}`)
	mac := hmac.New(sha256.New, []byte(auditTestSecret))
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("User-Agent", "GitHub-Hookshot/test")
	req.RemoteAddr = "192.0.2.10:4444"
	return req
}

// A signed GitHub push to an auto-deploy branch writes one
// deployment.trigger row (via=webhook) owned by the project owner, with
// provider, branch, commit, and repository, and the queued deployment
// carries that row's id so deployment.run.* links back.
func TestProcessPushEvent_WritesLinkedTriggerRow(t *testing.T) {
	t.Parallel()
	h, deploys, queue, audit := newAuditHandler(nil)

	rr := httptest.NewRecorder()
	h.HandleGitHub(rr, signedGitHubPush(t, "main"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(deploys.created) != 1 || len(queue.items) != 1 {
		t.Fatalf("created=%d enqueued=%d, want 1 and 1 (staging is not auto-deploy)", len(deploys.created), len(queue.items))
	}
	if len(audit.rows) != 1 {
		t.Fatalf("audit rows = %+v", audit.rows)
	}
	row, d := audit.rows[0], deploys.created[0]
	if row.id == "" || row.action != models.AuditActionDeploymentTrigger || row.userID != auditTestOwner ||
		row.resourceType != models.ResourceTypeDeployment || row.resourceID != d.ID {
		t.Errorf("row = %+v", row)
	}
	want := map[string]interface{}{
		"via": models.AuditViaWebhook, "provider": ProviderGitHub, "branch": "main", "commit": auditTestSHA,
		"repository": "acme/app", "project_id": "22222222-2222-4222-8222-222222222222",
		"environment_id": "33333333-3333-4333-8333-333333333333",
	}
	for k, v := range want {
		if row.details[k] != v {
			t.Errorf("details[%s] = %v, want %v", k, row.details[k], v)
		}
	}
	if row.ip != "192.0.2.10" || row.userAgent != "GitHub-Hookshot/test" {
		t.Errorf("ip=%q ua=%q", row.ip, row.userAgent)
	}
	if queue.items[0].auditID != row.id || queue.items[0].deploymentID != d.ID {
		t.Errorf("queued %+v, want audit id %s", queue.items[0], row.id)
	}
	if d.TriggerType != models.TriggerTypeWebhook {
		t.Errorf("trigger type = %s", d.TriggerType)
	}
	l := &models.AuditLog{Action: row.action, Details: mustJSON(t, row.details)}
	if l.Source() != models.AuditSourceWebhook {
		t.Errorf("source = %s, want webhook", l.Source())
	}
}

// A push to a branch no environment deploys, or a bad signature, writes
// no audit row.
func TestProcessPushEvent_NoDeployNoRow(t *testing.T) {
	t.Parallel()
	h, deploys, _, audit := newAuditHandler(nil)

	rr := httptest.NewRecorder()
	h.HandleGitHub(rr, signedGitHubPush(t, "feature"))
	if rr.Code != http.StatusOK || len(deploys.created) != 0 || len(audit.rows) != 0 {
		t.Fatalf("other branch: status=%d created=%d rows=%d", rr.Code, len(deploys.created), len(audit.rows))
	}

	req := signedGitHubPush(t, "main")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rr = httptest.NewRecorder()
	h.HandleGitHub(rr, req)
	if rr.Code != http.StatusUnauthorized || len(audit.rows) != 0 {
		t.Fatalf("bad signature: status=%d rows=%d", rr.Code, len(audit.rows))
	}
}

// When the queue refuses the deployment, deployment.trigger.failed links
// to the trigger row.
func TestProcessPushEvent_EnqueueFailureWritesFailedRow(t *testing.T) {
	t.Parallel()
	h, _, _, audit := newAuditHandler(errors.New("queue down"))

	rr := httptest.NewRecorder()
	h.HandleGitHub(rr, signedGitHubPush(t, "main"))
	if len(audit.rows) != 2 {
		t.Fatalf("audit rows = %+v", audit.rows)
	}
	start, failed := audit.rows[0], audit.rows[1]
	if failed.action != models.AuditActionDeploymentTrigger+".failed" || failed.details["audit_id"] != start.id ||
		failed.details["error"] != "queue down" || failed.details["via"] != models.AuditViaWebhook || failed.userID != auditTestOwner {
		t.Errorf("failed row = %+v", failed)
	}
}

// GitLab pushes are tagged provider=gitlab.
func TestProcessPushEvent_GitLabProvider(t *testing.T) {
	t.Parallel()
	h, _, _, audit := newAuditHandler(nil)
	body := []byte(`{"ref":"refs/heads/main","checkout_sha":"` + auditTestSHA + `","user_name":"dev",` +
		`"path_with_namespace":"acme/app","project":{"web_url":"` + auditTestRepo + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Token", auditTestSecret)
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	rr := httptest.NewRecorder()
	h.HandleGitLab(rr, req)
	if rr.Code != http.StatusOK || len(audit.rows) != 1 {
		t.Fatalf("status=%d rows=%+v", rr.Code, audit.rows)
	}
	if audit.rows[0].details["provider"] != ProviderGitLab || audit.rows[0].details["commit"] != auditTestSHA {
		t.Errorf("details = %#v", audit.rows[0].details)
	}
}

// Without WithAudit the push still deploys and nothing is linked.
func TestProcessPushEvent_NoRecorderStillDeploys(t *testing.T) {
	t.Parallel()
	h, deploys, queue, _ := newAuditHandler(nil)
	h.audit = nil
	rr := httptest.NewRecorder()
	h.HandleGitHub(rr, signedGitHubPush(t, "main"))
	if len(deploys.created) != 1 || len(queue.items) != 1 || queue.items[0].auditID != "" {
		t.Fatalf("created=%d queued=%+v", len(deploys.created), queue.items)
	}
}

func mustJSON(t *testing.T, d map[string]interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
