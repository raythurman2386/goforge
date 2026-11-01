package components

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestActivityResourceLinks(t *testing.T) {
	cases := []struct {
		rtype, rid string
		details    map[string]any
		want       templ.SafeURL
	}{
		{models.ResourceTypeProject, "p1", nil, "/projects/p1"},
		{models.ResourceTypeServiceInstance, "s1", nil, "/services/s1"},
		{models.ResourceTypeDeployment, "d1", map[string]any{"project_id": "p1"}, "/projects/p1/deployments/d1"},
		{models.ResourceTypeDeployment, "d1", map[string]any{"target": map[string]any{"project_id": "p2"}}, "/projects/p2/deployments/d1"},
		{models.ResourceTypeDeployment, "d1", nil, ""},
		{models.ResourceTypeEnvironment, "e1", map[string]any{"project_id": "p1"}, "/projects/p1"},
		{models.ResourceTypeContainer, "", map[string]any{"resource_ref": "abc123"}, "/containers/abc123"},
		{models.ResourceTypeAPIToken, "t1", nil, "/settings/profile"},
		{models.ResourceTypeGitSource, "g1", nil, "/settings/git"},
		{models.ResourceTypeUser, "u1", nil, ""},
	}
	for _, c := range cases {
		b, _ := json.Marshal(c.details)
		l := &models.AuditLog{ID: "a", Action: "x", ResourceType: c.rtype,
			ResourceID: sql.NullString{String: c.rid, Valid: c.rid != ""}, Details: b}
		if got := ActivityRows([]*models.AuditLog{l})[0].ResourceURL; got != c.want {
			t.Errorf("%s %s: href %q, want %q", c.rtype, c.rid, got, c.want)
		}
	}
}

func TestActivityRowSourceAndFailure(t *testing.T) {
	rows := ActivityRows([]*models.AuditLog{
		{ID: "1", Action: "service.delete", Details: json.RawMessage(`{"via":"ui"}`)},
		{ID: "2", Action: "api_v1.deploy_service.failed", Details: json.RawMessage(`{"error":"boom"}`)},
		{ID: "3", Action: "deployment.run.failed", UserAgent: sql.NullString{String: "goforge-mcp/1", Valid: true}, Details: json.RawMessage(`{"audit_id":"9"}`)},
	})
	if rows[0].Source != models.AuditSourceUI || rows[0].Failed {
		t.Errorf("ui row = %+v", rows[0])
	}
	if rows[1].Source != models.AuditSourceAPI || !rows[1].Failed || rows[1].Error != "boom" {
		t.Errorf("api row = %+v", rows[1])
	}
	if rows[2].Source != models.AuditSourceMCP || rows[2].RelatedURL != "/settings/activity?audit_id=9" {
		t.Errorf("mcp row = %+v", rows[2])
	}
}
