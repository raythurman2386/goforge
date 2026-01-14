package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/gocool/internal/auth"
	"github.com/raythurman2386/gocool/internal/models"
	"github.com/raythurman2386/gocool/internal/project"
	"github.com/raythurman2386/gocool/internal/web/templates/components"
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

	csrfToken := auth.GetCSRFToken(r)
	component := components.VariableEditor(envID, vars, csrfToken)
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
