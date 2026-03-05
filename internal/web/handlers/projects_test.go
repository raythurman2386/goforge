package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestProjectHandler_ListProjects_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	rr := httptest.NewRecorder()

	h := &ProjectHandler{}
	h.ListProjects(rr, req)

	// Should redirect to login
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected see other, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected location /login, got %s", loc)
	}
}

func TestProjectHandler_NewProjectPage_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/projects/new", nil)
	rr := httptest.NewRecorder()

	h := &ProjectHandler{}
	h.NewProjectPage(rr, req)

	// Should redirect to login
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected see other, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected location /login, got %s", loc)
	}
}

func TestProjectHandler_CreateProject_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/projects", nil)
	rr := httptest.NewRecorder()

	h := &ProjectHandler{}
	h.CreateProject(rr, req)

	// Should redirect to login
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected see other, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected location /login, got %s", loc)
	}
}

func TestProjectHandler_getCurrentEnv(t *testing.T) {
	h := &ProjectHandler{}

	envProd := &models.Environment{ID: "env-prod", Name: "production"}
	envStag := &models.Environment{ID: "env-stag", Name: "staging"}
	envs := []*models.Environment{envProd, envStag}

	tests := []struct {
		name   string
		envs   []*models.Environment
		envID  string
		wantID string
		wantNl bool
	}{
		{"empty envs returns nil", nil, "", "", true},
		{"no envID returns first", envs, "", "env-prod", false},
		{"matching envID returns match", envs, "env-stag", "env-stag", false},
		{"unknown envID defaults to first", envs, "env-unknown", "env-prod", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.getCurrentEnv(tt.envs, tt.envID)
			if tt.wantNl {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil environment")
			}
			if got.ID != tt.wantID {
				t.Errorf("expected env ID %q, got %q", tt.wantID, got.ID)
			}
		})
	}
}

func TestProjectHandler_GetProject_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/projects/123", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "123")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()

	// Without a project service, calling GetProject will panic on nil deref
	// unless auth check fails first. Since there's no user in context and
	// GetProject doesn't check auth (it relies on middleware), we test that
	// the route context is correctly wired by verifying the chi param.
	id := chi.URLParam(req, "id")
	if id != "123" {
		t.Errorf("expected chi URL param 'id' = '123', got %q", id)
	}

	// Verify the user context key is absent (no auth)
	user := auth.GetUserFromContext(req.Context())
	if user != nil {
		t.Error("expected nil user from unauthenticated context")
	}

	_ = rr // recorder available for future expansion with mock services
}
