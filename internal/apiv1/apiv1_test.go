package apiv1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

const (
	ownerID     = "11111111-1111-1111-1111-111111111111"
	otherID     = "22222222-2222-2222-2222-222222222222"
	projectID   = "33333333-3333-3333-3333-333333333333"
	foreignProj = "44444444-4444-4444-4444-444444444444"
	envID       = "55555555-5555-5555-5555-555555555555"
	svcID       = "66666666-6666-6666-6666-666666666666"
	protSvcID   = "0fe81d3f-50e8-4d5d-9802-25e52752c1e3"
	foreignSvc  = "77777777-7777-7777-7777-777777777777"
	ownDocker   = "dock-own"
	protDocker  = "dock-prot"
	otherDocker = "dock-other"
	dbErrDocker = "dock-dberr" // owned, but its row lookup fails
	adminID     = "88888888-8888-8888-8888-888888888888"
)

// ---- fakes: each embeds its interface so unused methods panic ----

type fakeTokens struct{ byHash map[string]*models.APIToken }

func (f *fakeTokens) GetByTokenHash(_ context.Context, hash string) (*models.APIToken, error) {
	if t, ok := f.byHash[hash]; ok {
		return t, nil
	}
	return nil, repositories.ErrNotFound
}
func (f *fakeTokens) UpdateLastUsed(context.Context, string) error { return nil }

type fakeUsers struct{}

func (fakeUsers) GetByID(_ context.Context, id string) (*models.User, error) {
	role := models.RoleUser
	if id == adminID {
		role = models.RoleAdmin
	}
	return &models.User{ID: id, Email: id[:4] + "@example.com", Role: role}, nil
}

type fakeProjects struct {
	Projects
	mu    sync.Mutex
	vars  []*models.EnvVariable
	added []*models.EnvVariable
}

func (f *fakeProjects) GetProject(_ context.Context, id string) (*models.Project, error) {
	switch id {
	case projectID:
		return &models.Project{ID: projectID, UserID: ownerID, Name: "app", DefaultBranch: "main"}, nil
	case foreignProj:
		return &models.Project{ID: foreignProj, UserID: otherID, Name: "theirs"}, nil
	}
	return nil, repositories.ErrNotFound
}

func (f *fakeProjects) GetEnvironment(_ context.Context, id string) (*models.Environment, error) {
	if id == envID {
		return &models.Environment{ID: envID, ProjectID: projectID, Name: "production"}, nil
	}
	return nil, repositories.ErrNotFound
}

func (f *fakeProjects) ListEnvironments(context.Context, string) ([]*models.Environment, error) {
	return []*models.Environment{{ID: envID, ProjectID: projectID, Name: "production"}}, nil
}

func (f *fakeProjects) ListEnvVariables(context.Context, string) ([]*models.EnvVariable, error) {
	return f.vars, nil
}

func (f *fakeProjects) AddEnvVariable(_ context.Context, v *models.EnvVariable) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, v)
	return nil
}

type fakeInstances struct {
	Instances
	tmpl      *services.ServiceTemplate
	restarted []string
	updated   []string
	imaged    []string
	// imageAuditIDs is models.AuditIDFrom(ctx) for each UpdateInstanceImage.
	imageAuditIDs []string
	// updateErr, when set, is what UpdateInstanceConfig returns.
	updateErr error
}

func (f *fakeInstances) GetInstance(_ context.Context, id string) (*models.ServiceInstance, error) {
	cfg := json.RawMessage(`{"name":"db","password":"hunter2-secret","enc_key":"fixed","size":"small"}`)
	switch id {
	case svcID, protSvcID:
		return &models.ServiceInstance{ID: id, UserID: ownerID, TemplateName: "pg", Name: "db", Config: cfg, Status: models.ServiceStatusRunning}, nil
	case foreignSvc:
		return &models.ServiceInstance{ID: id, UserID: otherID, TemplateName: "pg", Name: "x", Config: cfg}, nil
	case failedSvcID:
		return &models.ServiceInstance{ID: id, UserID: ownerID, TemplateName: "pg", Name: "broken", Config: cfg,
			Status: models.ServiceStatusFailed, LastError: sql.NullString{String: failedSvcLastError, Valid: true}}, nil
	}
	return nil, repositories.ErrNotFound
}

func (f *fakeInstances) GetContainer(context.Context, string) (*models.Container, error) {
	return nil, repositories.ErrNotFound
}

func (f *fakeInstances) Template(string) *services.ServiceTemplate { return f.tmpl }

func (f *fakeInstances) RestartInstance(_ context.Context, id string) error {
	f.restarted = append(f.restarted, id)
	return nil
}

func (f *fakeInstances) UpdateInstanceConfig(_ context.Context, id string, _ map[string]interface{}) (*models.ServiceInstance, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.updated = append(f.updated, id)
	return &models.ServiceInstance{ID: id}, nil
}

func (f *fakeInstances) UpdateInstanceImage(ctx context.Context, id string) (*models.ServiceInstance, error) {
	f.imaged = append(f.imaged, id)
	f.imageAuditIDs = append(f.imageAuditIDs, models.AuditIDFrom(ctx))
	return &models.ServiceInstance{ID: id}, nil
}

type fakeContainers struct{ Containers }

func (fakeContainers) OwnedByUser(_ context.Context, dockerID, userID string) (bool, error) {
	return userID == ownerID && (dockerID == ownDocker || dockerID == protDocker || dockerID == dbErrDocker), nil
}

func (fakeContainers) GetByDockerID(_ context.Context, dockerID string) (*models.Container, error) {
	switch dockerID {
	case ownDocker:
		return &models.Container{ID: "c1", DockerID: ownDocker, Name: "goforge-service-" + svcID}, nil
	case protDocker:
		return &models.Container{ID: "c2", DockerID: protDocker, Name: "goforge-service-" + protSvcID,
			ServiceInstanceID: sql.NullString{String: protSvcID, Valid: true}}, nil
	case dbErrDocker:
		return nil, errors.New("connection reset by peer")
	}
	return nil, repositories.ErrNotFound
}

func (fakeContainers) UpdateStatus(context.Context, string, models.ContainerStatus) error { return nil }

type fakeDocker struct {
	Docker
	stopped, restarted []string
}

func (f *fakeDocker) StopContainer(_ context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeDocker) RestartContainer(_ context.Context, id string) error {
	f.restarted = append(f.restarted, id)
	return nil
}

type fakeAudit struct {
	mu   sync.Mutex
	rows []*models.AuditLog
	fail bool
}

func (f *fakeAudit) Create(_ context.Context, l *models.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("db down")
	}
	// Mirror the unique index from migration 023.
	if d := detailsOf(l); d.IdempotencyKey != "" && f.findApprovalLocked(l.UserID.String, l.Action, d.ApprovalRef, d.IdempotencyKey) != nil {
		return repositories.ErrAlreadyExists
	}
	f.rows = append(f.rows, l)
	return nil
}

// snapshot returns the rows written so far; safe while a detached change
// is still writing its outcome.
func (f *fakeAudit) snapshot() []*models.AuditLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*models.AuditLog(nil), f.rows...)
}

func (f *fakeAudit) ListFiltered(context.Context, repositories.AuditFilter) ([]*models.AuditLog, error) {
	return f.rows, nil
}

type auditDetails struct {
	ApprovalRef    string `json:"approval_ref"`
	IdempotencyKey string `json:"idempotency_key"`
	AuditID        string `json:"audit_id"`
}

func detailsOf(l *models.AuditLog) auditDetails {
	var d auditDetails
	_ = json.Unmarshal(l.Details, &d)
	return d
}

func (f *fakeAudit) FindApproval(_ context.Context, userID, action, ref, key string) (*models.AuditLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.findApprovalLocked(userID, action, ref, key), nil
}

func (f *fakeAudit) findApprovalLocked(userID, action, ref, key string) *models.AuditLog {
	for _, l := range f.rows {
		d := detailsOf(l)
		if l.UserID.String == userID && l.Action == action && d.ApprovalRef == ref && d.IdempotencyKey == key {
			return l
		}
	}
	return nil
}

func (f *fakeAudit) FindOutcome(_ context.Context, userID, action, auditID string) (*models.AuditLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.rows {
		if l.UserID.String == userID && (l.Action == action+".completed" || l.Action == action+".failed") && detailsOf(l).AuditID == auditID {
			return l, nil
		}
	}
	return nil, nil
}

// ---- harness ----

type harness struct {
	srv       http.Handler
	projects  *fakeProjects
	instances *fakeInstances
	docker    *fakeDocker
	audit     *fakeAudit
}

var tokenScopes = map[string][]string{
	"tok-full":      {models.ScopeFull},
	"tok-read":      {models.ScopeRead},
	"tok-write":     {models.ScopeWrite},
	"tok-protected": {models.ScopeWrite, models.ScopeProtected},
	"tok-other":     {models.ScopeWrite},
	"tok-admin":     {models.ScopeRead},
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	tokens := &fakeTokens{byHash: map[string]*models.APIToken{}}
	for raw, scopes := range tokenScopes {
		uid := ownerID
		switch raw {
		case "tok-other":
			uid = otherID
		case "tok-admin":
			uid = adminID
		}
		tokens.byHash[auth.HashAPIToken(raw)] = &models.APIToken{ID: "id-" + raw, UserID: uid, Name: raw, Scopes: scopes}
	}
	h := &harness{
		projects: &fakeProjects{vars: []*models.EnvVariable{
			{ID: "v1", Key: "PUBLIC_URL", Value: "https://app.example", EnvironmentID: envID},
			{ID: "v2", Key: "API_KEY", Value: "sk-live-very-secret", IsSecret: true, EnvironmentID: envID},
		}},
		instances: &fakeInstances{tmpl: &services.ServiceTemplate{
			Name: "pg",
			Config: []services.ConfigField{
				{Name: "name", Type: "string"},
				{Name: "password", Type: services.FieldTypePassword},
				{Name: "enc_key", Type: "string", Immutable: true},
				{Name: "size", Type: "string"},
			},
		}},
		docker: &fakeDocker{},
		audit:  &fakeAudit{},
	}
	h.srv = New(Deps{
		Tokens:            tokens,
		Users:             fakeUsers{},
		Projects:          h.projects,
		Instances:         h.instances,
		Containers:        fakeContainers{},
		Docker:            h.docker,
		Audit:             h.audit,
		Backups:           fakeBackups{},
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		ProtectedServices: []string{protSvcID},
	}).Routes()
	return h
}

func (h *harness) do(t *testing.T, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "192.0.2.10:5555"
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

// ---- tests ----

func TestAuthRequiresBearer(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodGet, "/whoami", "", "")
	if rec.Code != http.StatusUnauthorized || errCode(out) != codeUnauthorized {
		t.Fatalf("no bearer: got %d %v", rec.Code, out)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate")
	}
	rec, _ = h.do(t, http.MethodGet, "/whoami", "not-a-token", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: got %d", rec.Code)
	}
}

func TestSessionUserIsIgnored(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, &models.User{ID: ownerID})
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req.WithContext(ctx))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session-only request got %d, want 401", rec.Code)
	}
}

func TestScopes(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		token, method, path string
		want                int
	}{
		{"tok-read", http.MethodGet, "/projects/" + projectID, http.StatusOK},
		{"tok-write", http.MethodGet, "/projects/" + projectID, http.StatusOK},
		{"tok-full", http.MethodGet, "/projects/" + projectID, http.StatusOK},
		{"tok-read", http.MethodPost, "/services/" + svcID + "/restart", http.StatusForbidden},
		{"tok-write", http.MethodPost, "/services/" + svcID + "/restart", http.StatusOK},
		{"tok-full", http.MethodPost, "/services/" + svcID + "/restart", http.StatusOK},
	}
	for _, c := range cases {
		rec, out := h.do(t, c.method, c.path, c.token, "")
		if rec.Code != c.want {
			t.Errorf("%s %s %s: got %d want %d (%v)", c.token, c.method, c.path, rec.Code, c.want, out)
		}
		if c.want == http.StatusForbidden && errCode(out) != codeInsufficientScope {
			t.Errorf("%s: code %q", c.token, errCode(out))
		}
	}
}

func TestWhoamiReportsGrantsAndProtectedList(t *testing.T) {
	h := newHarness(t)
	_, out := h.do(t, http.MethodGet, "/whoami", "tok-full", "")
	grants, _ := out["grants"].(map[string]any)
	if grants["read"] != true || grants["write"] != true || grants["protected"] != false {
		t.Errorf("full token grants = %v (full must never imply protected)", grants)
	}
	list, _ := out["protected_services"].([]any)
	if len(list) != 1 || list[0] != protSvcID {
		t.Errorf("protected_services = %v", list)
	}
}

func TestOwnershipParity(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/projects/" + foreignProj, "", http.StatusNotFound},
		{http.MethodGet, "/projects/" + foreignProj + "/environments", "", http.StatusNotFound},
		{http.MethodPost, "/projects/" + foreignProj + "/environments/" + envID + "/variables", `{"key":"A","value":"b"}`, http.StatusNotFound},
		{http.MethodGet, "/services/" + foreignSvc, "", http.StatusForbidden},
		{http.MethodPost, "/services/" + foreignSvc + "/restart", "", http.StatusForbidden},
		{http.MethodPost, "/containers/" + otherDocker + "/stop", "", http.StatusForbidden},
		{http.MethodPost, "/containers/" + otherDocker + "/restart", "", http.StatusForbidden},
	}
	for _, c := range cases {
		rec, out := h.do(t, c.method, c.path, "tok-write", c.body)
		if rec.Code != c.want {
			t.Errorf("%s %s: got %d want %d (%v)", c.method, c.path, rec.Code, c.want, out)
		}
	}
	// The owner of the foreign project sees it.
	rec, _ := h.do(t, http.MethodGet, "/services/"+svcID, "tok-other", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("other user on owner's service: %d", rec.Code)
	}
}

func TestEnvVarsMasked(t *testing.T) {
	h := newHarness(t)
	rec, _ := h.do(t, http.MethodGet, "/projects/"+projectID+"/environments/"+envID+"/variables", "tok-read", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-live-very-secret") {
		t.Fatal("secret env var value leaked")
	}
	if !strings.Contains(body, services.MaskedValue) || !strings.Contains(body, "https://app.example") {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestServiceConfigMasked(t *testing.T) {
	h := newHarness(t)
	rec, _ := h.do(t, http.MethodGet, "/services/"+svcID, "tok-read", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "hunter2-secret") {
		t.Fatal("service password leaked")
	}
}

func TestDryRunDefaultHasNoSideEffects(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodPost, "/services/"+svcID+"/restart", "tok-write", "")
	if rec.Code != http.StatusOK || out["dry_run"] != true || out["plan"] == "" {
		t.Fatalf("dry run: %d %v", rec.Code, out)
	}
	rec, _ = h.do(t, http.MethodPost, "/containers/"+ownDocker+"/stop", "tok-write", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop dry run: %d", rec.Code)
	}
	rec, _ = h.do(t, http.MethodPost, "/projects/"+projectID+"/environments/"+envID+"/variables", "tok-write", `{"key":"NEW","value":"v"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set var dry run: %d", rec.Code)
	}
	if len(h.instances.restarted)+len(h.docker.stopped)+len(h.projects.added)+len(h.audit.rows) != 0 {
		t.Fatalf("dry run had side effects: %+v %+v %+v %d", h.instances.restarted, h.docker.stopped, h.projects.added, len(h.audit.rows))
	}
}

func TestApprovalRefRequired(t *testing.T) {
	h := newHarness(t)
	for _, body := range []string{`{"dry_run":false}`, `{"dry_run":false,"approval_ref":"   "}`, `{"dry_run":false,"approval_ref":"a\nb"}`} {
		rec, out := h.do(t, http.MethodPost, "/services/"+svcID+"/restart", "tok-write", body)
		if rec.Code != http.StatusBadRequest || errCode(out) != codeApprovalRequired {
			t.Errorf("%s: got %d %v", body, rec.Code, out)
		}
	}
	if len(h.instances.restarted) != 0 || len(h.audit.rows) != 0 {
		t.Fatal("refused request had side effects")
	}
}

func TestRealRunWritesAuditBeforeExecuting(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodPost, "/containers/"+ownDocker+"/restart", "tok-write", `{"dry_run":false,"approval_ref":"t1250u"}`)
	if rec.Code != http.StatusOK || out["audit_id"] == "" {
		t.Fatalf("got %d %v", rec.Code, out)
	}
	// The approval row first, then the outcome row written by the change.
	if len(h.docker.restarted) != 1 || len(h.audit.rows) != 2 {
		t.Fatalf("restarted=%v audit=%d", h.docker.restarted, len(h.audit.rows))
	}
	if done := h.audit.rows[1]; done.Action != "api_v1.restart_container.completed" ||
		!strings.Contains(string(done.Details), out["audit_id"].(string)) {
		t.Errorf("outcome row %s %s", done.Action, done.Details)
	}
	row := h.audit.rows[0]
	if row.Action != "api_v1.restart_container" || !row.UserID.Valid || row.UserID.String != ownerID {
		t.Errorf("audit row %+v", row)
	}
	if row.ResourceID.Valid {
		t.Error("docker id must not go in the uuid resource_id column")
	}
	var details map[string]any
	_ = json.Unmarshal(row.Details, &details)
	if details["approval_ref"] != "t1250u" || details["token_id"] != "id-tok-write" || details["resource_ref"] != ownDocker {
		t.Errorf("details %v", details)
	}
	if row.IPAddress.String != "192.0.2.10" {
		t.Errorf("ip %q", row.IPAddress.String)
	}
}

func TestAuditFailureBlocksMutation(t *testing.T) {
	h := newHarness(t)
	h.audit.fail = true
	rec, _ := h.do(t, http.MethodPost, "/services/"+svcID+"/restart", "tok-write", `{"dry_run":false,"approval_ref":"t1250u"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", rec.Code)
	}
	if len(h.instances.restarted) != 0 {
		t.Fatal("mutation ran without an audit row")
	}
}

func TestProtectedRefusal(t *testing.T) {
	h := newHarness(t)
	realRun := `{"dry_run":false,"approval_ref":"t1250u"}`
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/containers/" + protDocker + "/stop", realRun},
		{http.MethodPost, "/containers/" + protDocker + "/stop", ""}, // dry run is refused too
		{http.MethodPost, "/services/" + protSvcID + "/image", realRun},
		{http.MethodPatch, "/services/" + protSvcID + "/config", `{"config":{"size":"large"},"dry_run":false,"approval_ref":"t1250u"}`},
	}
	for _, tok := range []string{"tok-write", "tok-full"} {
		for _, c := range cases {
			rec, out := h.do(t, c.method, c.path, tok, c.body)
			if rec.Code != http.StatusForbidden || errCode(out) != codeProtected {
				t.Errorf("%s %s %s: got %d %v", tok, c.method, c.path, rec.Code, out)
			}
		}
	}
	if len(h.docker.stopped)+len(h.instances.imaged)+len(h.instances.updated)+len(h.audit.rows) != 0 {
		t.Fatal("protected refusal reached docker or audit")
	}

	// With the protected scope the same calls go through.
	rec, out := h.do(t, http.MethodPost, "/containers/"+protDocker+"/stop", "tok-protected", realRun)
	if rec.Code != http.StatusOK {
		t.Fatalf("protected scope stop: %d %v", rec.Code, out)
	}
	if len(h.docker.stopped) != 1 {
		t.Fatal("stop did not run with protected scope")
	}
}

// A container row that cannot be read after the ownership check must not be
// treated as an unprotected, unredacted container: the API answers 500.
func TestContainerLookupErrorFailsClosed(t *testing.T) {
	h := newHarness(t)
	realRun := `{"dry_run":false,"approval_ref":"t1254u"}`
	cases := []struct{ method, path, token, body string }{
		{http.MethodPost, "/containers/" + dbErrDocker + "/stop", "tok-write", ""},
		{http.MethodPost, "/containers/" + dbErrDocker + "/stop", "tok-write", realRun},
		{http.MethodPost, "/containers/" + dbErrDocker + "/stop", "tok-protected", realRun},
		{http.MethodPost, "/containers/" + dbErrDocker + "/restart", "tok-write", realRun},
		{http.MethodGet, "/containers/" + dbErrDocker, "tok-read", ""},
		{http.MethodGet, "/containers/" + dbErrDocker + "/logs", "tok-read", ""},
	}
	for _, c := range cases {
		rec, out := h.do(t, c.method, c.path, c.token, c.body)
		if rec.Code != http.StatusInternalServerError || errCode(out) != codeInternal {
			t.Errorf("%s %s %s: got %d %v, want 500 %s", c.token, c.method, c.path, rec.Code, out, codeInternal)
		}
	}
	if len(h.docker.stopped)+len(h.docker.restarted)+len(h.audit.rows) != 0 {
		t.Fatal("lookup error reached docker or audit")
	}
}

func TestRestartOnProtectedAllowedWithWrite(t *testing.T) {
	h := newHarness(t)
	realRun := `{"dry_run":false,"approval_ref":"t1250u"}`
	rec, out := h.do(t, http.MethodPost, "/services/"+protSvcID+"/restart", "tok-write", realRun)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart_service: %d %v", rec.Code, out)
	}
	rec, out = h.do(t, http.MethodPost, "/containers/"+protDocker+"/restart", "tok-write", realRun)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart_container: %d %v", rec.Code, out)
	}
	if len(h.instances.restarted) != 1 || len(h.docker.restarted) != 1 {
		t.Fatal("restarts did not run")
	}
}

func TestUpdateConfigDiff(t *testing.T) {
	h := newHarness(t)
	path := "/services/" + svcID + "/config"
	rec, out := h.do(t, http.MethodPatch, path, "tok-write", `{"config":{"enc_key":"changed"}}`)
	if rec.Code != http.StatusUnprocessableEntity || errCode(out) != codeImmutable {
		t.Errorf("immutable: %d %v", rec.Code, out)
	}
	rec, _ = h.do(t, http.MethodPatch, path, "tok-write", `{"config":{"bogus":"x"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", rec.Code)
	}
	rec, _ = h.do(t, http.MethodPatch, path, "tok-write", `{"config":{"password":"new-pass-value","size":"large"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("diff dry run: %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "new-pass-value") || strings.Contains(body, "hunter2-secret") {
		t.Fatal("password leaked in diff")
	}
	if !strings.Contains(body, `"large"`) {
		t.Errorf("diff missing size change: %s", body)
	}
	rec, out = h.do(t, http.MethodPatch, path, "tok-write", `{"config":{"size":"small","enc_key":"fixed"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(out["plan"].(string), "no changes") {
		t.Errorf("no-op: %d %v", rec.Code, out)
	}
	if len(h.instances.updated) != 0 {
		t.Fatal("dry runs updated config")
	}
}

// The instance reads as running, so the handler's own busy check passes, but
// another edit wins the status compare-and-swap first (#48). The loser is a 409.
func TestUpdateConfigLostRaceIsConflict(t *testing.T) {
	h := newHarness(t)
	h.instances.updateErr = services.ErrInstanceBusy
	rec, out := h.do(t, http.MethodPatch, "/services/"+svcID+"/config", "tok-write",
		`{"config":{"size":"large"},"dry_run":false,"approval_ref":"t1250u"}`)
	if rec.Code != http.StatusConflict || errCode(out) != codeConflict {
		t.Fatalf("got %d %v, want 409 %s", rec.Code, out, codeConflict)
	}
	if len(h.instances.updated) != 0 {
		t.Fatal("losing edit recorded as applied")
	}
}

func TestSetEnvVarValueNeverEchoedOrAudited(t *testing.T) {
	h := newHarness(t)
	rec, _ := h.do(t, http.MethodPost, "/projects/"+projectID+"/environments/"+envID+"/variables", "tok-write",
		`{"key":"DB_PASS","value":"plaintext-pass","is_secret":true,"dry_run":false,"approval_ref":"t1250u"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "plaintext-pass") {
		t.Fatal("value echoed")
	}
	if len(h.audit.rows) != 2 {
		t.Fatalf("audit rows = %d, want approval and outcome", len(h.audit.rows))
	}
	for _, row := range h.audit.rows {
		if strings.Contains(string(row.Details), "plaintext-pass") {
			t.Fatalf("value audited in %s", row.Action)
		}
	}
	if len(h.projects.added) != 1 || h.projects.added[0].Value != "plaintext-pass" {
		t.Fatal("variable not saved")
	}
	rec, _ = h.do(t, http.MethodPost, "/projects/"+projectID+"/environments/"+envID+"/variables", "tok-write", `{"key":"bad key","value":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad key: %d", rec.Code)
	}
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	h := newHarness(t)
	rec, out := h.do(t, http.MethodGet, "/nope", "tok-read", "")
	if rec.Code != http.StatusNotFound || errCode(out) == "" {
		t.Fatalf("got %d %v", rec.Code, out)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
}
