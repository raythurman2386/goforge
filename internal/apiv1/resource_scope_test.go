package apiv1

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/models"
)

const ownProj2 = "33333333-3333-4333-8333-333333333332"

// tokenRestrictions are the restricted harness tokens: {project_ids, service_ids}.
var tokenRestrictions = map[string][2][]string{
	"tok-rsvc":  {nil, {svcID}},
	"tok-rproj": {{projectID}, nil},
	// Upper-case on purpose: IDs compare case-insensitively.
	"tok-rboth": {{strings.ToUpper(projectID)}, {strings.ToUpper(svcID)}},
}

func (f *fakeProjects) ListProjects(ctx context.Context, userID string) ([]*models.Project, error) {
	var out []*models.Project
	for _, id := range []string{projectID, ownProj2, foreignProj} {
		if p, err := f.GetProject(ctx, id); err == nil && p.UserID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f fakeContainers) ListByUserID(ctx context.Context, userID string) ([]*models.Container, error) {
	var out []*models.Container
	for _, id := range []string{ownDocker, protDocker, uiProtDock} {
		if owned, _ := f.OwnedByUser(ctx, id, userID); owned {
			c, _ := f.GetByDockerID(ctx, id)
			out = append(out, c)
		}
	}
	return out, nil
}

// A restricted token reaches its listed services and projects and gets 403
// resource_out_of_scope on every other one, reads and writes alike, before
// any dry-run plan, audit row, or Docker call.
func TestRestrictedTokenOutOfScope(t *testing.T) {
	h := newHarness(t)
	realRun := `{"dry_run":false,"approval_ref":"t1668u"}`
	refused := []struct{ token, method, path, body string }{
		// tok-rsvc: services [svcID] only.
		{"tok-rsvc", http.MethodGet, "/services/" + protSvcID, ""},
		{"tok-rsvc", http.MethodPost, "/services/" + protSvcID + "/restart", realRun},
		{"tok-rsvc", http.MethodPost, "/services/" + uiProtSvcID + "/restart", ""},
		{"tok-rsvc", http.MethodPatch, "/services/" + uiProtSvcID + "/config", `{"config":{"size":"large"},"dry_run":false,"approval_ref":"t1668u"}`},
		{"tok-rsvc", http.MethodPost, "/containers/" + protDocker + "/restart", realRun},
		{"tok-rsvc", http.MethodGet, "/containers/" + uiProtDock + "/logs", ""},
		{"tok-rsvc", http.MethodGet, "/projects/" + projectID, ""},
		{"tok-rsvc", http.MethodPost, "/projects/" + projectID + "/deployments", realRun},
		// tok-rproj: projects [projectID] only.
		{"tok-rproj", http.MethodGet, "/projects/" + ownProj2, ""},
		{"tok-rproj", http.MethodGet, "/projects/" + ownProj2 + "/environments", ""},
		{"tok-rproj", http.MethodPost, "/projects/" + ownProj2 + "/deployments", realRun},
		{"tok-rproj", http.MethodGet, "/services/" + svcID, ""},
		{"tok-rproj", http.MethodPost, "/services/" + svcID + "/restart", realRun},
		{"tok-rproj", http.MethodPost, "/containers/" + ownDocker + "/stop", realRun},
		// Linking touches the service too, so it must be listed.
		{"tok-rproj", http.MethodPost, "/projects/" + projectID + "/services", `{"service_instance_id":"` + svcID + `","dry_run":false,"approval_ref":"t1668u"}`},
		{"tok-rproj", http.MethodDelete, "/projects/" + projectID + "/services/" + svcID, realRun},
		// Host-wide routes.
		{"tok-rsvc", http.MethodGet, "/dashboard/stats", ""},
		{"tok-rsvc", http.MethodGet, "/backups", ""},
		{"tok-rproj", http.MethodGet, "/git-sources", ""},
		{"tok-rproj", http.MethodGet, "/git-sources/x/repos", ""},
		{"tok-rsvc", http.MethodPost, "/services", `{"template":"pg","name":"new","dry_run":false,"approval_ref":"t1668u"}`},
		// The audit log needs a listed resource_id.
		{"tok-rsvc", http.MethodGet, "/audit-log", ""},
		{"tok-rsvc", http.MethodGet, "/audit-log?resource_id=" + protSvcID, ""},
		{"tok-rsvc", http.MethodGet, "/audit-log?resource_id=" + svcID + "&all_users=true", ""},
	}
	for _, c := range refused {
		rec, out := h.do(t, c.method, c.path, c.token, c.body)
		if rec.Code != http.StatusForbidden || errCode(out) != codeOutOfScope {
			t.Errorf("%s %s %s: got %d %v, want 403 %s", c.token, c.method, c.path, rec.Code, out, codeOutOfScope)
		}
	}
	if n := len(h.docker.stopped) + len(h.docker.restarted) + len(h.instances.restarted) + len(h.instances.updated) + len(h.audit.snapshot()); n != 0 {
		t.Fatalf("an out-of-scope request reached docker, the instance service, or the audit log (%d)", n)
	}

	// Ownership still answers first: a foreign project is 404, not 403.
	if rec, _ := h.do(t, http.MethodGet, "/projects/"+foreignProj, "tok-rproj", ""); rec.Code != http.StatusNotFound {
		t.Errorf("foreign project with a restricted token: %d, want 404", rec.Code)
	}
	if rec, out := h.do(t, http.MethodGet, "/services/"+foreignSvc, "tok-rsvc", ""); rec.Code != http.StatusForbidden || errCode(out) != codeForbidden {
		t.Errorf("foreign service with a restricted token: %d %v, want 403 forbidden", rec.Code, out)
	}
}

// The listed resources still work, under the usual scopes.
func TestRestrictedTokenInScope(t *testing.T) {
	h := newHarness(t)
	allowed := []struct{ token, method, path, body string }{
		{"tok-rsvc", http.MethodGet, "/services/" + svcID, ""},
		{"tok-rsvc", http.MethodPost, "/services/" + svcID + "/restart", ""},
		{"tok-rsvc", http.MethodPost, "/containers/" + ownDocker + "/restart", ""},
		{"tok-rsvc", http.MethodPost, "/containers/" + ownDocker + "/stop", `{"dry_run":false,"approval_ref":"t1668u-stop"}`},
		{"tok-rsvc", http.MethodGet, "/audit-log?resource_id=" + svcID, ""},
		{"tok-rproj", http.MethodGet, "/projects/" + projectID, ""},
		{"tok-rproj", http.MethodGet, "/projects/" + projectID + "/environments/" + envID + "/variables", ""},
		{"tok-rproj", http.MethodPost, "/projects/" + projectID + "/environments/" + envID + "/variables", `{"key":"X","value":"y"}`},
		{"tok-rboth", http.MethodGet, "/projects/" + projectID, ""},
		{"tok-rboth", http.MethodGet, "/services/" + svcID, ""},
	}
	for _, c := range allowed {
		rec, out := h.do(t, c.method, c.path, c.token, c.body)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s %s: got %d %v, want 200", c.token, c.method, c.path, rec.Code, out)
		}
	}
	if !slices.Equal(h.docker.stopped, []string{ownDocker}) {
		t.Errorf("stopped = %v", h.docker.stopped)
	}
	// Scopes still apply on top: a restricted token without protected
	// cannot stop a protected service even when it is listed.
	tokenRestrictionsSet(t, "tok-rsvc", nil, []string{protSvcID})
	h2 := newHarness(t)
	if rec, out := h2.do(t, http.MethodPost, "/containers/"+protDocker+"/stop", "tok-rsvc", ""); rec.Code != http.StatusForbidden || errCode(out) != codeProtected {
		t.Errorf("listed protected service without the protected scope: %d %v, want 403 %s", rec.Code, out, codeProtected)
	}
}

// tokenRestrictionsSet swaps one harness token's lists for the test.
func tokenRestrictionsSet(t *testing.T, raw string, projects, services []string) {
	t.Helper()
	old := tokenRestrictions[raw]
	tokenRestrictions[raw] = [2][]string{projects, services}
	t.Cleanup(func() { tokenRestrictions[raw] = old })
}

// Lists only show what a restricted token may touch; a host-wide token
// still sees everything it owns.
func TestRestrictedTokenListsAreFiltered(t *testing.T) {
	h := newHarness(t)
	ids := func(out map[string]any, key, field string) []string {
		var got []string
		list, _ := out[key].([]any)
		for _, it := range list {
			m, _ := it.(map[string]any)
			s, _ := m[field].(string)
			got = append(got, s)
		}
		return got
	}
	_, out := h.do(t, http.MethodGet, "/services", "tok-rsvc", "")
	if got := ids(out, "services", "id"); !slices.Equal(got, []string{svcID}) {
		t.Errorf("tok-rsvc services = %v", got)
	}
	_, out = h.do(t, http.MethodGet, "/projects", "tok-rsvc", "")
	if got := ids(out, "projects", "id"); len(got) != 0 {
		t.Errorf("tok-rsvc projects = %v, want none", got)
	}
	_, out = h.do(t, http.MethodGet, "/containers", "tok-rsvc", "")
	if got := ids(out, "containers", "docker_id"); !slices.Equal(got, []string{ownDocker}) {
		t.Errorf("tok-rsvc containers = %v", got)
	}
	_, out = h.do(t, http.MethodGet, "/projects", "tok-rproj", "")
	if got := ids(out, "projects", "id"); !slices.Equal(got, []string{projectID}) {
		t.Errorf("tok-rproj projects = %v", got)
	}
	_, out = h.do(t, http.MethodGet, "/projects", "tok-write", "")
	if got := ids(out, "projects", "id"); !slices.Equal(got, []string{projectID, ownProj2}) {
		t.Errorf("host-wide projects = %v", got)
	}
	_, out = h.do(t, http.MethodGet, "/services", "tok-write", "")
	if got := ids(out, "services", "id"); len(got) != 3 {
		t.Errorf("host-wide services = %v", got)
	}
}

// whoami reports the lists (never null) and filters the protected list to
// the token's services.
func TestWhoamiReportsRestriction(t *testing.T) {
	h := newHarness(t)
	_, out := h.do(t, http.MethodGet, "/whoami", "tok-rsvc", "")
	tok, _ := out["token"].(map[string]any)
	if tok["restricted"] != true {
		t.Fatalf("restricted = %v", tok["restricted"])
	}
	if p, ok := tok["project_ids"].([]any); !ok || len(p) != 0 {
		t.Errorf("project_ids = %#v, want []", tok["project_ids"])
	}
	if s, _ := tok["service_ids"].([]any); len(s) != 1 || s[0] != svcID {
		t.Errorf("service_ids = %v", tok["service_ids"])
	}
	if ps, _ := out["protected_services"].([]any); len(ps) != 0 {
		t.Errorf("protected_services = %v, want only listed services (none protected)", ps)
	}

	_, out = h.do(t, http.MethodGet, "/whoami", "tok-write", "")
	tok, _ = out["token"].(map[string]any)
	p, pok := tok["project_ids"].([]any)
	s, sok := tok["service_ids"].([]any)
	if tok["restricted"] != false || !pok || !sok || len(p)+len(s) != 0 {
		t.Errorf("host-wide whoami token = %v", tok)
	}
	if ps, _ := out["protected_services"].([]any); len(ps) != 2 {
		t.Errorf("host-wide protected_services = %v", ps)
	}
}

// A deployment container follows its project: in scope only when the
// project is listed. Untracked or unreadable ones are out (fail closed).
func TestContainerInScopeByDeployment(t *testing.T) {
	deps := &fakeDeployments{}
	h := New(Deps{Projects: &fakeProjects{}, Deployments: deps})
	depC := &models.Container{DockerID: "app", Name: "goforge-app", DeploymentID: sql.NullString{String: deplID, Valid: true}}
	lostC := &models.Container{DockerID: "lost", Name: "goforge-app", DeploymentID: sql.NullString{String: "missing", Valid: true}}
	bare := &models.Container{DockerID: "bare", Name: "something"}
	cases := []struct {
		name string
		tok  *models.APIToken
		c    *models.Container
		want bool
	}{
		{"host-wide", &models.APIToken{}, bare, true},
		{"listed project", &models.APIToken{ProjectIDs: []string{projectID}}, depC, true},
		{"other project", &models.APIToken{ProjectIDs: []string{ownProj2}}, depC, false},
		{"services only", &models.APIToken{ServiceIDs: []string{svcID}}, depC, false},
		{"unreadable deployment", &models.APIToken{ProjectIDs: []string{projectID}}, lostC, false},
		{"untracked", &models.APIToken{ProjectIDs: []string{projectID}}, bare, false},
		{"nil token", nil, depC, false},
	}
	for _, c := range cases {
		if got := h.containerInScope(context.Background(), c.tok, c.c); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func (f *fakeDocker) ListContainers(context.Context, bool) ([]container.Summary, error) {
	return nil, nil
}
