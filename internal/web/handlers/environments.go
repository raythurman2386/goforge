package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
)

// EnvironmentHandler handles environment related requests.
type EnvironmentHandler struct {
	projectService *project.Service
}

// NewEnvironmentHandler creates a new EnvironmentHandler.
func NewEnvironmentHandler(ps *project.Service) *EnvironmentHandler {
	return &EnvironmentHandler{
		projectService: ps,
	}
}

// ListEnvironments renders the environment list for a project.
func (h *EnvironmentHandler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	envs, err := h.projectService.ListEnvironments(r.Context(), projectID)
	if err != nil {
		http.Error(w, "failed to list environments", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	_, _ = fmt.Fprintf(w, "<div>Found %d environments for project %s</div>", len(envs), projectID)
}

// CreateEnvironment handles environment creation.
func (h *EnvironmentHandler) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	name := r.FormValue("name")

	env := &models.Environment{
		ProjectID: projectID,
		Name:      name,
	}

	if err := h.projectService.CreateEnvironment(r.Context(), env); err != nil {
		http.Error(w, "failed to create environment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusCreated)
}

// UpdateEnvironment handles environment updates.
func (h *EnvironmentHandler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "envId")

	env, err := h.projectService.GetEnvironment(r.Context(), envID)
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	if domain := r.FormValue("domain"); domain != "" {
		env.Domain.String = domain
		env.Domain.Valid = true
	} else {
		env.Domain.Valid = false
	}

	if branch := r.FormValue("branch"); branch != "" {
		env.Branch.String = branch
		env.Branch.Valid = true
	} else {
		env.Branch.Valid = false
	}

	if portStr := r.FormValue("port"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err == nil && port > 0 && port < 65536 {
			env.Port.Int32 = int32(port) //nolint:gosec // port checked to be within valid range
			env.Port.Valid = true
		}
	}

	if err := h.projectService.UpdateEnvironment(r.Context(), env); err != nil {
		http.Error(w, "failed to update environment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// NewEnvironmentModal renders the modal to create a new environment.
func (h *EnvironmentHandler) NewEnvironmentModal(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	csrfToken := auth.GetCSRFToken(r)
	component := components.CreateEnvironmentModal(projectID, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render modal", http.StatusInternalServerError)
	}
}

// GetVariables renders the variable editor for an environment.
func (h *EnvironmentHandler) GetVariables(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "envId")
	vars, err := h.projectService.ListEnvVariables(r.Context(), envID)
	if err != nil {
		http.Error(w, "failed to list variables", http.StatusInternalServerError)
		return
	}

	// Look up the environment to get the project ID (needed for the sync button URL).
	projectID := ""
	env, err := h.projectService.GetEnvironment(r.Context(), envID)
	if err == nil {
		projectID = env.ProjectID
	}

	csrfToken := auth.GetCSRFToken(r)
	component := components.VariableEditor(envID, vars, csrfToken, projectID)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render variable editor", http.StatusInternalServerError)
	}
}

// AddVariable handles adding a new environment variable.
func (h *EnvironmentHandler) AddVariable(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "envId")
	key := r.FormValue("key")
	value := r.FormValue("value")
	isSecret := r.FormValue("is_secret") == "on"

	v := &models.EnvVariable{
		EnvironmentID: envID,
		Key:           key,
		Value:         value,
		IsSecret:      isSecret,
	}

	if err := h.projectService.AddEnvVariable(r.Context(), v); err != nil {
		http.Error(w, "failed to add variable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusCreated)
}

// NewVariableModal renders the modal to add a new environment variable.
func (h *EnvironmentHandler) NewVariableModal(w http.ResponseWriter, r *http.Request) {
	envID := chi.URLParam(r, "envId")
	csrfToken := auth.GetCSRFToken(r)
	component := components.AddVariableModal(envID, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render modal", http.StatusInternalServerError)
	}
}

// SyncEnvFromRepo fetches .env.example from the project's repo and seeds new variables
// into the given environment (idempotent — existing keys are skipped).
func (h *EnvironmentHandler) SyncEnvFromRepo(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	envID := chi.URLParam(r, "envId")

	_, err := h.projectService.SeedEnvVarsFromRepo(r.Context(), envID, projectID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to sync env vars: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// DeleteVariable removes an environment variable by ID.
func (h *EnvironmentHandler) DeleteVariable(w http.ResponseWriter, r *http.Request) {
	varID := chi.URLParam(r, "varId")

	if err := h.projectService.DeleteEnvVariable(r.Context(), varID); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete variable: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}
