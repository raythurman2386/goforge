package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

const linkSvcID = "cdcdcdcd-cdcd-cdcd-cdcd-cdcdcdcdcdcd"

type linkInstances struct{ *fakeInstances }

func (linkInstances) GetInstance(_ context.Context, id string) (*models.ServiceInstance, error) {
	return &models.ServiceInstance{ID: id, UserID: ownerID, TemplateName: "postgres", Name: "db",
		Status:         models.ServiceStatusRunning,
		ConnectionInfo: json.RawMessage(`{"internal":"postgres://db/app","external":"postgres://pub/app"}`)}, nil
}

// flakyProjects fails to save any variable whose key contains failOn.
type flakyProjects struct {
	*fakeProjects
	failOn string
}

func (f flakyProjects) AddEnvVariable(ctx context.Context, v *models.EnvVariable) error {
	if strings.Contains(v.Key, f.failOn) {
		return errors.New("insert failed")
	}
	return f.fakeProjects.AddEnvVariable(ctx, v)
}

type fakeLinks struct {
	ServiceLinks
	linked int
}

func (f *fakeLinks) Link(context.Context, string, string) error { f.linked++; return nil }

// When a link's variables only partly save, the answer is still a success
// (the link stands) but says so: partial true and the keys in failed_keys.
func TestLinkServiceReportsFailedKeys(t *testing.T) {
	h := newHarness(t)
	links := &fakeLinks{}
	tokens := &fakeTokens{byHash: map[string]*models.APIToken{
		auth.HashAPIToken("tok-write"): {ID: "id", UserID: ownerID, Name: "w", Scopes: []string{models.ScopeWrite}},
	}}
	h.srv = New(Deps{
		Tokens: tokens, Users: fakeUsers{}, Audit: h.audit, Links: links,
		Projects:  flakyProjects{fakeProjects: h.projects, failOn: "EXTERNAL"},
		Instances: linkInstances{h.instances},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Routes()

	rec, out := h.do(t, http.MethodPost, "/projects/"+projectID+"/services", "tok-write",
		`{"service_instance_id":"`+linkSvcID+`","dry_run":false,"approval_ref":"t1632u-link"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	res, _ := out["result"].(map[string]any)
	failedKeys, _ := res["failed_keys"].([]any)
	addedKeys, _ := res["added_keys"].([]any)
	if links.linked != 1 || res["partial"] != true || len(failedKeys) != 1 || len(addedKeys) != 1 {
		t.Fatalf("result = %v (linked %d), want one added, one failed, partial", res, links.linked)
	}
	if k, _ := failedKeys[0].(string); !strings.Contains(k, "EXTERNAL") {
		t.Errorf("failed_keys = %v", failedKeys)
	}
	if strings.Contains(rec.Body.String(), "postgres://") {
		t.Error("connection value echoed")
	}

	// All keys saved: partial false and failed_keys empty, not missing.
	h.srv = New(Deps{
		Tokens: tokens, Users: fakeUsers{}, Audit: h.audit, Links: links,
		Projects: h.projects, Instances: linkInstances{h.instances},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Routes()
	_, out = h.do(t, http.MethodPost, "/projects/"+projectID+"/services", "tok-write",
		`{"service_instance_id":"`+linkSvcID+`","dry_run":false,"approval_ref":"t1632u-link2"}`)
	res, _ = out["result"].(map[string]any)
	if fk, ok := res["failed_keys"].([]any); !ok || len(fk) != 0 || res["partial"] != false {
		t.Errorf("clean link result = %v", res)
	}
}
