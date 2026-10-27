package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/deployments"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/projects"
)

// DeploymentHandler handles deployment related requests.
type DeploymentHandler struct {
	deployService   *deploy.Service
	projectService  *project.Service
	rollbackManager *deploy.RollbackManager
	worker          *deploy.Worker
	logger          *slog.Logger
}

// NewDeploymentHandler creates a new DeploymentHandler.
func NewDeploymentHandler(ds *deploy.Service, ps *project.Service, rm *deploy.RollbackManager, w *deploy.Worker, logger *slog.Logger) *DeploymentHandler {
	return &DeploymentHandler{
		deployService:   ds,
		projectService:  ps,
		rollbackManager: rm,
		worker:          w,
		logger:          logger,
	}
}

// TriggerDeployment triggers a new deployment.
func (h *DeploymentHandler) TriggerDeployment(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	projectID := chi.URLParam(r, "id")

	// Verify project ownership
	proj, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil || proj.UserID != user.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	envID := r.URL.Query().Get("env_id")

	if envID == "" {
		// Try to find default environment
		envs, err := h.projectService.ListEnvironments(r.Context(), projectID)
		if err != nil || len(envs) == 0 {
			http.Error(w, "environment not found", http.StatusBadRequest)
			return
		}
		envID = envs[0].ID
	}

	d := &models.Deployment{
		EnvironmentID: envID,
		Status:        models.DeploymentStatusPending,
		TriggerType:   models.TriggerTypeManual,
	}

	if err := h.deployService.CreateDeployment(r.Context(), d); err != nil {
		http.Error(w, "failed to create deployment record", http.StatusInternalServerError)
		return
	}

	if err := h.worker.Enqueue(r.Context(), d.ID); err != nil {
		http.Error(w, "failed to enqueue deployment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/projects/"+projectID+"/deployments")
	w.WriteHeader(http.StatusOK)
}

// GetDeployment renders the deployment detail page.
func (h *DeploymentHandler) GetDeployment(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	projectID := chi.URLParam(r, "id")
	deploymentID := chi.URLParam(r, "deploymentID")

	// Get the project and verify ownership
	proj, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil || proj.UserID != user.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	// Get the deployment
	deployment, err := h.deployService.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return
	}

	// Get the environment
	env, err := h.projectService.GetEnvironment(r.Context(), deployment.EnvironmentID)
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	// Get deployment logs
	logs, err := h.deployService.GetLogs(r.Context(), deploymentID)
	if err != nil {
		h.logger.Warn("failed to get deployment logs", "deployment_id", deploymentID, "error", err)
		logs = nil
	}

	// Determine if rollback is possible
	canRollback := h.rollbackManager != nil && h.rollbackManager.CanRollback(deployment)

	csrfToken := auth.GetCSRFToken(r)
	props := deployments.DetailProps{
		Deployment:  deployment,
		Project:     proj,
		Environment: env,
		Logs:        logs,
		CanRollback: canRollback,
		CSRFToken:   csrfToken,
	}

	component := deployments.Detail(props)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render deployment detail page", http.StatusInternalServerError)
	}
}

// RollbackDeployment handles rollback requests.
func (h *DeploymentHandler) RollbackDeployment(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	projectID := chi.URLParam(r, "id")
	deploymentID := chi.URLParam(r, "deploymentID")

	// Verify project ownership
	proj, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil || proj.UserID != user.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	if h.rollbackManager == nil {
		http.Error(w, "rollback not available", http.StatusServiceUnavailable)
		return
	}

	// Get the deployment to find the environment
	deployment, err := h.deployService.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return
	}

	// Perform the rollback
	info, err := h.rollbackManager.Rollback(r.Context(), deployment.EnvironmentID, "manual rollback")
	if err != nil {
		http.Error(w, "rollback failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to deployment detail page with rollback info
	_ = info // TODO: Show rollback info in UI via toast
	w.Header().Set("HX-Redirect", "/projects/"+projectID+"/deployments/"+deploymentID)
	w.WriteHeader(http.StatusOK)
}

// CancelDeployment handles deployment cancellation requests.
func (h *DeploymentHandler) CancelDeployment(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	projectID := chi.URLParam(r, "id")
	deploymentID := chi.URLParam(r, "deploymentID")

	// Verify project ownership
	proj, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil || proj.UserID != user.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	// Get the deployment
	deployment, err := h.deployService.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return
	}

	// Check if deployment can be canceled
	if deployment.IsTerminal() {
		http.Error(w, "deployment is already in a terminal state", http.StatusBadRequest)
		return
	}

	errorMsg := "canceled by user"
	if err := h.deployService.Cancel(r.Context(), deploymentID, errorMsg); err != nil {
		http.Error(w, "failed to cancel deployment: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if h.worker != nil {
		if dropErr := h.worker.DropPending(r.Context(), deploymentID); dropErr != nil && h.logger != nil {
			h.logger.Error("failed to drop pending deployment", "deployment_id", deploymentID, "error", dropErr)
		}
	}

	// Redirect back to deployment detail page
	w.Header().Set("HX-Redirect", "/projects/"+projectID+"/deployments/"+deploymentID)
	w.WriteHeader(http.StatusOK)
}

// GetDeploymentStatus returns the current deployment status for HTMX polling.
// When the deployment is no longer active, it returns an empty div to stop the banner.
func (h *DeploymentHandler) GetDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	projectID := chi.URLParam(r, "id")
	deploymentID := chi.URLParam(r, "deploymentID")

	// Verify project ownership
	proj, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil || proj.UserID != user.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	deployment, err := h.deployService.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return
	}

	// If deployment is in a terminal state, return an empty div that doesn't poll
	if deployment.IsTerminal() {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(`<div id="deployment-status-banner"></div>`)); err != nil {
			http.Error(w, "failed to write response", http.StatusInternalServerError)
		}
		return
	}

	// Return updated banner component
	component := projects.DeploymentStatusBanner(projectID, deployment)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render deployment status", http.StatusInternalServerError)
	}
}
