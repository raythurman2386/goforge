package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

const (
	scopeProj    = "33333333-3333-4333-8333-333333333333"
	scopeSvc     = "66666666-6666-4666-8666-666666666666"
	scopeForeign = "99999999-9999-4999-8999-999999999999"
)

type fakeTokenProjects struct{}

func (fakeTokenProjects) ListProjects(_ context.Context, userID string) ([]*models.Project, error) {
	if userID != tokenOwner {
		return nil, nil
	}
	return []*models.Project{{ID: scopeProj, UserID: tokenOwner, Name: "web"}}, nil
}

type fakeTokenServices struct{}

func (fakeTokenServices) ListInstances(_ context.Context, userID string) ([]*models.ServiceInstance, error) {
	if userID != tokenOwner {
		return nil, nil
	}
	return []*models.ServiceInstance{{ID: scopeSvc, UserID: tokenOwner, Name: "db"}}, nil
}

func scopedTokenHandler() (*SettingsHandler, *fakeTokenStore, *fakeAudit) {
	h, store, a := tokenHandler()
	return h.WithTokenResources(fakeTokenProjects{}, fakeTokenServices{}), store, a
}

func TestCreateAPIToken_Restricted(t *testing.T) {
	h, store, a := scopedTokenHandler()
	w := httptest.NewRecorder()
	h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", url.Values{
		"name": {"agent"}, "scopes": {"read", "write"},
		"project_ids": {strings.ToUpper(scopeProj)}, "service_ids": {scopeSvc, scopeSvc},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	tok := store.only(t)
	if !slices.Equal(tok.ProjectIDs, []string{scopeProj}) || !slices.Equal(tok.ServiceIDs, []string{scopeSvc}) {
		t.Fatalf("lists = %v %v, want normalized and deduped", tok.ProjectIDs, tok.ServiceIDs)
	}
	d := a.details[0]
	if fmtList(d["project_ids"]) != scopeProj || fmtList(d["service_ids"]) != scopeSvc {
		t.Fatalf("audit details = %v", d)
	}
}

func fmtList(v any) string {
	if l, ok := v.([]string); ok {
		return strings.Join(l, ",")
	}
	return ""
}

func TestCreateAPIToken_HostWideByDefault(t *testing.T) {
	h, store, a := scopedTokenHandler()
	w := httptest.NewRecorder()
	h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", url.Values{"name": {"ci"}, "scopes": {"read"}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	if tok := store.only(t); tok.IsRestricted() {
		t.Fatalf("token restricted without a pick: %+v", tok)
	}
	if _, ok := a.details[0]["project_ids"]; ok {
		t.Fatal("host-wide token audit carries resource lists")
	}
}

func TestCreateAPIToken_RestrictedRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
		want int
	}{
		{"foreign project", url.Values{"scopes": {"read"}, "project_ids": {scopeForeign}}, http.StatusBadRequest},
		{"foreign service", url.Values{"scopes": {"read"}, "service_ids": {scopeForeign}}, http.StatusBadRequest},
		{"not a uuid", url.Values{"scopes": {"read"}, "project_ids": {"web"}}, http.StatusBadRequest},
		{"full scope", url.Values{"scopes": {"full"}, "project_ids": {scopeProj}}, http.StatusBadRequest},
		{"no scope picked", url.Values{"project_ids": {scopeProj}}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, store, a := scopedTokenHandler()
			tc.form.Set("name", "x")
			w := httptest.NewRecorder()
			h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", tc.form))
			if w.Code != tc.want || len(store.rows) != 0 || len(a.actions) != 0 {
				t.Fatalf("status = %d rows = %d audit = %v, want %d and nothing stored", w.Code, len(store.rows), a.actions, tc.want)
			}
		})
	}
}

func TestCreateAPIToken_RestrictedWithoutListers503(t *testing.T) {
	h, store, _ := tokenHandler()
	w := httptest.NewRecorder()
	h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", url.Values{"name": {"x"}, "scopes": {"read"}, "project_ids": {scopeProj}}))
	if w.Code != http.StatusServiceUnavailable || len(store.rows) != 0 {
		t.Fatalf("status = %d rows = %d, want 503 and nothing stored", w.Code, len(store.rows))
	}
}

func TestListAPITokens_ShowsRestriction(t *testing.T) {
	h, store, _ := scopedTokenHandler()
	store.rows["r"] = &models.APIToken{ID: "r", UserID: tokenOwner, Name: "scoped", Scopes: []string{"read"},
		ProjectIDs: []string{scopeProj}, ServiceIDs: []string{scopeSvc, scopeForeign}}
	store.rows["h"] = &models.APIToken{ID: "h", UserID: tokenOwner, Name: "host", Scopes: []string{"read"}}
	w := httptest.NewRecorder()
	h.ListAPITokens(w, tokenRequest(http.MethodGet, "/settings/tokens", "", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	for _, want := range []string{"data-token-restricted", `data-token-project="` + scopeProj + `"`, "project: web",
		`data-token-service="` + scopeSvc + `"`, "service: db", "service: 99999999 (deleted)", "data-token-host-wide"} {
		if !strings.Contains(body, want) {
			t.Errorf("token list missing %q", want)
		}
	}
}

func TestNewAPITokenModal_OffersOwnedResources(t *testing.T) {
	h, _, _ := scopedTokenHandler()
	w := httptest.NewRecorder()
	h.NewAPITokenModal(w, tokenRequest(http.MethodGet, "/settings/tokens/new", "", nil))
	body := w.Body.String()
	for _, want := range []string{"data-token-restrict", `id="token-project-` + scopeProj + `"`, `name="project_ids"`,
		`id="token-service-` + scopeSvc + `"`, `name="service_ids"`} {
		if !strings.Contains(body, want) {
			t.Errorf("modal missing %q", want)
		}
	}
}
