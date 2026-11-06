package handlers

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
)

type fakeRollbackAPI struct {
	info *deploy.RollbackInfo
	err  error
}

func (f *fakeRollbackAPI) CanRollback(*models.Deployment) bool { return true }
func (f *fakeRollbackAPI) Rollback(context.Context, string, string) (*deploy.RollbackInfo, error) {
	return f.info, f.err
}

func deploymentFeedbackFixture() (*DeploymentHandler, *models.User, *models.Deployment, *models.Deployment) {
	user := &models.User{ID: "user-a"}
	current := &models.Deployment{ID: "11111111-current", EnvironmentID: "env-a", Status: models.DeploymentStatusRolledBack}
	target := &models.Deployment{ID: "22222222-target", EnvironmentID: "env-a", Status: models.DeploymentStatusRunning, CommitSHA: sql.NullString{String: "abc1234", Valid: true}}
	deps := &fakeDeploymentAPI{items: map[string]*models.Deployment{current.ID: current, target.ID: target}}
	projects := &fakeProjectAPI{
		projects: map[string]*models.Project{"proj-a": {ID: "proj-a", UserID: user.ID, Name: "alpha"}},
		envs:     map[string]*models.Environment{"env-a": {ID: "env-a", ProjectID: "proj-a"}},
	}
	return &DeploymentHandler{deployService: deps, projectService: projects, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, user, current, target
}

func TestRollbackDeployment_RedirectNamesReturnedTarget(t *testing.T) {
	h, user, current, target := deploymentFeedbackFixture()
	h.rollbackManager = &fakeRollbackAPI{info: &deploy.RollbackInfo{FromDeploymentID: current.ID, ToDeploymentID: target.ID}}
	rr := httptest.NewRecorder()
	h.RollbackDeployment(rr, deploymentRequest(user, "POST", "proj-a", current.ID, ""))
	if rr.Code != 200 {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	want := "/projects/proj-a/deployments/" + current.ID + "?rollback=" + target.ID
	if got := rr.Header().Get("HX-Redirect"); got != want {
		t.Fatalf("HX-Redirect = %q, want %q", got, want)
	}

	rr = httptest.NewRecorder()
	h.GetDeployment(rr, deploymentRequest(user, "GET", "proj-a", current.ID, "rollback="+target.ID))
	body := rr.Body.String()
	for _, text := range []string{"Rollback complete", "22222222", "/projects/proj-a/deployments/" + target.ID, "View restored deployment"} {
		if !strings.Contains(body, text) {
			t.Errorf("post-redirect feedback lacks %q", text)
		}
	}

	// Returning without the one-time query shows the actual rolled-back row and
	// its logs, rather than presenting another deployment as the current one.
	rr = httptest.NewRecorder()
	h.GetDeployment(rr, deploymentRequest(user, "GET", "proj-a", current.ID, ""))
	body = rr.Body.String()
	if strings.Contains(body, "Rollback complete") || !strings.Contains(body, "rolled_back") {
		t.Error("return visit did not show the current row without stale feedback")
	}
}

func TestRollbackResultRejectsCrossEnvironmentTarget(t *testing.T) {
	h, user, current, _ := deploymentFeedbackFixture()
	h.deployService.(*fakeDeploymentAPI).items["foreign"] = &models.Deployment{ID: "foreign", EnvironmentID: "env-b"}
	h.projectService.(*fakeProjectAPI).envs["env-b"] = &models.Environment{ID: "env-b", ProjectID: "proj-b"}
	rr := httptest.NewRecorder()
	h.GetDeployment(rr, deploymentRequest(user, "GET", "proj-a", current.ID, "rollback=foreign"))
	if strings.Contains(rr.Body.String(), "Rollback complete") {
		t.Fatal("query parameter exposed feedback for another environment")
	}
}

func TestRollbackDeployment_FailureDoesNotRedirect(t *testing.T) {
	h, user, current, _ := deploymentFeedbackFixture()
	h.rollbackManager = &fakeRollbackAPI{err: errors.New("restore failed")}
	rr := httptest.NewRecorder()
	h.RollbackDeployment(rr, deploymentRequest(user, "POST", "proj-a", current.ID, ""))
	if rr.Code != 500 || !strings.Contains(rr.Body.String(), "rollback failed: restore failed") || rr.Header().Get("HX-Redirect") != "" {
		t.Fatalf("status = %d headers = %v body = %q", rr.Code, rr.Header(), rr.Body.String())
	}
}

func TestGetDeploymentStatus_ActiveBannerShowsElapsedWithoutPercent(t *testing.T) {
	h, user, current, _ := deploymentFeedbackFixture()
	current.Status = models.DeploymentStatusBuilding
	current.StartedAt = sql.NullTime{Time: time.Now().Add(-90 * time.Second), Valid: true}
	rr := httptest.NewRecorder()
	h.GetDeploymentStatus(rr, deploymentRequest(user, "GET", "proj-a", current.ID, ""))
	body := rr.Body.String()
	for _, want := range []string{`data-deployment-stage="building"`, "Elapsed: 1m 30s"} {
		if !strings.Contains(body, want) {
			t.Errorf("banner lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "%") || strings.Contains(body, "completion") {
		t.Error("banner claims fabricated completion")
	}
}
