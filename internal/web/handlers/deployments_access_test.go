package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

const idorLogMessage = "secret-build-log-from-project-a"

type fakeDeploymentAPI struct {
	items    map[string]*models.Deployment
	logs     []*models.DeploymentLog
	canceled int
	created  int
}

func (f *fakeDeploymentAPI) CreateDeployment(context.Context, *models.Deployment) error {
	f.created++
	return nil
}

func (f *fakeDeploymentAPI) GetDeployment(_ context.Context, id string) (*models.Deployment, error) {
	deployment, ok := f.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return deployment, nil
}

func (f *fakeDeploymentAPI) GetLogs(context.Context, string) ([]*models.DeploymentLog, error) {
	return f.logs, nil
}

func (f *fakeDeploymentAPI) Cancel(context.Context, string, string) error {
	f.canceled++
	return nil
}

type fakeProjectAPI struct {
	projects map[string]*models.Project
	envs     map[string]*models.Environment
}

func (f *fakeProjectAPI) GetProject(_ context.Context, id string) (*models.Project, error) {
	project, ok := f.projects[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return project, nil
}

func (f *fakeProjectAPI) ListEnvironments(_ context.Context, projectID string) ([]*models.Environment, error) {
	var out []*models.Environment
	for _, env := range f.envs {
		if env.ProjectID == projectID {
			out = append(out, env)
		}
	}
	return out, nil
}

func (f *fakeProjectAPI) GetEnvironment(_ context.Context, id string) (*models.Environment, error) {
	env, ok := f.envs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return env, nil
}

func deploymentRequest(user *models.User, method, projectID, deploymentID, rawQuery string) *http.Request {
	target := "/projects/" + projectID + "/deployments"
	if deploymentID != "" {
		target += "/" + deploymentID
	}
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(method, target, nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", projectID)
	if deploymentID != "" {
		route.URLParams.Add("deploymentID", deploymentID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, route)
	ctx = context.WithValue(ctx, auth.UserContextKey, user)
	return req.WithContext(ctx)
}

// TestDeploymentHandler_CrossUserDeploymentIDOR proves user B cannot read or
// mutate user A's deployment by putting A's deployment id under B's project path.
func TestDeploymentHandler_CrossUserDeploymentIDOR(t *testing.T) {
	owner := &models.User{ID: "user-a"}
	other := &models.User{ID: "user-b"}
	projectA := &models.Project{ID: "proj-a", UserID: owner.ID, Name: "alpha"}
	projectA2 := &models.Project{ID: "proj-a2", UserID: owner.ID, Name: "alpha-other"}
	projectB := &models.Project{ID: "proj-b", UserID: other.ID, Name: "beta"}
	envA := &models.Environment{ID: "env-a", ProjectID: projectA.ID, Name: "production"}
	deployment := &models.Deployment{
		ID:            "dep-a",
		EnvironmentID: envA.ID,
		Status:        models.DeploymentStatusPending,
	}
	deps := &fakeDeploymentAPI{
		items: map[string]*models.Deployment{deployment.ID: deployment},
		logs:  []*models.DeploymentLog{{Message: idorLogMessage}},
	}
	projects := &fakeProjectAPI{
		projects: map[string]*models.Project{
			projectA.ID:  projectA,
			projectA2.ID: projectA2,
			projectB.ID:  projectB,
		},
		envs: map[string]*models.Environment{envA.ID: envA},
	}
	handler := &DeploymentHandler{
		deployService:  deps,
		projectService: projects,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	denied := []struct {
		name    string
		user    *models.User
		project string
		call    func(http.ResponseWriter, *http.Request)
		method  string
	}{
		{"other user get", other, projectB.ID, handler.GetDeployment, http.MethodGet},
		{"other user rollback", other, projectB.ID, handler.RollbackDeployment, http.MethodPost},
		{"other user cancel", other, projectB.ID, handler.CancelDeployment, http.MethodPost},
		{"other user status", other, projectB.ID, handler.GetDeploymentStatus, http.MethodGet},
		{"owner other project get", owner, projectA2.ID, handler.GetDeployment, http.MethodGet},
		{"owner other project rollback", owner, projectA2.ID, handler.RollbackDeployment, http.MethodPost},
		{"owner other project cancel", owner, projectA2.ID, handler.CancelDeployment, http.MethodPost},
		{"owner other project status", owner, projectA2.ID, handler.GetDeploymentStatus, http.MethodGet},
	}
	for _, tt := range denied {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := deploymentRequest(tt.user, tt.method, tt.project, deployment.ID, "")
			tt.call(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 body=%q", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), idorLogMessage) {
				t.Fatal("response included the other project's deployment log")
			}
		})
	}
	if deps.canceled != 0 {
		t.Fatalf("cancel ran %d times for a deployment outside the path project", deps.canceled)
	}

	t.Run("other user cannot trigger into the foreign environment", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := deploymentRequest(other, http.MethodPost, projectB.ID, "", "env_id="+envA.ID)
		handler.TriggerDeployment(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rr.Code)
		}
		if deps.created != 0 {
			t.Fatal("created a deployment on another project's environment")
		}
	})

	t.Run("owner rollback passes the project check", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := deploymentRequest(owner, http.MethodPost, projectA.ID, deployment.ID, "")
		handler.RollbackDeployment(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("owner status passes the project check", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := deploymentRequest(owner, http.MethodGet, projectA.ID, deployment.ID, "")
		handler.GetDeploymentStatus(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 body=%q", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "Deployment in progress") {
			t.Fatalf("body = %q, want the status banner", rr.Body.String())
		}
	})

	t.Run("owner get passes the project check", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := deploymentRequest(owner, http.MethodGet, projectA.ID, deployment.ID, "")
		handler.GetDeployment(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 body=%q", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), idorLogMessage) {
			t.Fatal("owner response omitted the deployment log")
		}
	})

	t.Run("owner cancel passes the project check", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := deploymentRequest(owner, http.MethodPost, projectA.ID, deployment.ID, "")
		handler.CancelDeployment(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 body=%q", rr.Code, rr.Body.String())
		}
		if deps.canceled != 1 {
			t.Fatalf("cancel count = %d, want 1", deps.canceled)
		}
	})
}
