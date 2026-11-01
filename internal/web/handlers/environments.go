package handlers

import (
	"fmt"
	"log/slog"
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
	logger         *slog.Logger
	audit          uiAudit
}

// WithAudit records environment and variable changes in the audit log
// (via=ui). Rows name the variable key, never its value.
func (h *EnvironmentHandler) WithAudit(rec UIAuditRecorder) *EnvironmentHandler {
	h.audit = uiAudit{rec: rec}
	return h
}

// NewEnvironmentHandler creates a new EnvironmentHandler.
func NewEnvironmentHandler(ps *project.Service, logger *slog.Logger) *EnvironmentHandler {
	return &EnvironmentHandler{
		projectService: ps,
		logger:         logger,
	}
}

// verifyProjectOwnership checks that the project (from URL param "id") belongs to the authenticated user.
// Returns the project on success, or nil if unauthorized (after writing the error response).
func (h *EnvironmentHandler) verifyProjectOwnership(w http.ResponseWriter, r *http.Request, user *models.User) *models.Project {
	projectID := chi.URLParam(r, "id")
	if projectID == "" {
		return nil
	}
	p, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil || p.UserID != user.ID {
		http.Error(w, "project not found", http.StatusNotFound)
		return nil
	}
	return p
}

// verifyEnvOwnership checks that an environment (looked up by envID) belongs to a project
// that the authenticated user owns. Returns the environment on success.
func (h *EnvironmentHandler) verifyEnvOwnership(w http.ResponseWriter, r *http.Request, user *models.User, envID string) *models.Environment {
	env, err := h.projectService.GetEnvironment(r.Context(), envID)
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return nil
	}
	// Verify the environment's project belongs to the user
	p, err := h.projectService.GetProject(r.Context(), env.ProjectID)
	if err != nil || p.UserID != user.ID {
		http.Error(w, "environment not found", http.StatusNotFound)
		return nil
	}
	return env
}

// ListEnvironments renders the environment list for a project.
func (h *EnvironmentHandler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	p := h.verifyProjectOwnership(w, r, user)
	if p == nil {
		return
	}

	envs, err := h.projectService.ListEnvironments(r.Context(), p.ID)
	if err != nil {
		http.Error(w, "failed to list environments", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	_, _ = fmt.Fprintf(w, "<div>Found %d environments for project %s</div>", len(envs), p.ID)
}

// CreateEnvironment handles environment creation.
func (h *EnvironmentHandler) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	p := h.verifyProjectOwnership(w, r, user)
	if p == nil {
		return
	}

	name := r.FormValue("name")
	if name == "" || len(name) > maxInputLength {
		http.Error(w, "environment name is required and must be under 255 characters", http.StatusBadRequest)
		return
	}

	env := &models.Environment{
		ProjectID: p.ID,
		Name:      name,
	}

	if err := h.projectService.CreateEnvironment(r.Context(), env); err != nil {
		http.Error(w, "failed to create environment", http.StatusInternalServerError)
		return
	}
	h.audit.record(r, models.AuditActionEnvironmentCreate, models.ResourceTypeEnvironment, env.ID,
		map[string]interface{}{"project_id": p.ID, "name": name})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusCreated)
}

// UpdateEnvironment handles environment updates.
func (h *EnvironmentHandler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	envID := chi.URLParam(r, "envId")

	env := h.verifyEnvOwnership(w, r, user, envID)
	if env == nil {
		return
	}

	if domain := r.FormValue("domain"); domain != "" {
		if !isValidDomain(domain) {
			http.Error(w, "invalid domain format", http.StatusBadRequest)
			return
		}
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
	h.audit.record(r, models.AuditActionEnvironmentUpdate, models.ResourceTypeEnvironment, env.ID,
		map[string]interface{}{"project_id": env.ProjectID, "name": env.Name})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// NewEnvironmentModal renders the modal to create a new environment.
func (h *EnvironmentHandler) NewEnvironmentModal(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	p := h.verifyProjectOwnership(w, r, user)
	if p == nil {
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := components.CreateEnvironmentModal(p.ID, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render modal", http.StatusInternalServerError)
	}
}

// GetVariables renders the variable editor for an environment.
func (h *EnvironmentHandler) GetVariables(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	envID := chi.URLParam(r, "envId")

	env := h.verifyEnvOwnership(w, r, user, envID)
	if env == nil {
		return
	}

	vars, err := h.projectService.ListEnvVariables(r.Context(), envID)
	if err != nil {
		http.Error(w, "failed to list variables", http.StatusInternalServerError)
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := components.VariableEditor(envID, vars, csrfToken, env.ProjectID)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render variable editor", http.StatusInternalServerError)
	}
}

// AddVariable handles adding a new environment variable.
func (h *EnvironmentHandler) AddVariable(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	envID := chi.URLParam(r, "envId")

	env := h.verifyEnvOwnership(w, r, user, envID)
	if env == nil {
		return
	}

	key := r.FormValue("key")
	value := r.FormValue("value")
	isSecret := r.FormValue("is_secret") == "on"

	if key == "" || len(key) > maxEnvVarKeyLen {
		http.Error(w, "variable key is required and must be under 128 characters", http.StatusBadRequest)
		return
	}

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
	h.audit.record(r, models.AuditActionEnvVarSet, models.ResourceTypeEnvironment, envID,
		map[string]interface{}{"project_id": env.ProjectID, "key": key, "is_secret": isSecret})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusCreated)
}

// NewVariableModal renders the modal to add a new environment variable.
func (h *EnvironmentHandler) NewVariableModal(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	envID := chi.URLParam(r, "envId")

	env := h.verifyEnvOwnership(w, r, user, envID)
	if env == nil {
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := components.AddVariableModal(envID, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render modal", http.StatusInternalServerError)
	}
}

// SyncEnvFromRepo fetches .env.example from the project's repo and seeds new variables
// into the given environment (idempotent — existing keys are skipped).
func (h *EnvironmentHandler) SyncEnvFromRepo(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	p := h.verifyProjectOwnership(w, r, user)
	if p == nil {
		return
	}

	envID := chi.URLParam(r, "envId")

	// Verify environment belongs to this project
	env := h.verifyEnvOwnership(w, r, user, envID)
	if env == nil {
		return
	}
	if env.ProjectID != p.ID {
		http.Error(w, "environment does not belong to this project", http.StatusForbidden)
		return
	}

	added, err := h.projectService.SeedEnvVarsFromRepo(r.Context(), envID, p.ID)
	if err != nil {
		http.Error(w, "failed to sync environment variables from repository", http.StatusInternalServerError)
		return
	}
	h.audit.record(r, models.AuditActionEnvVarSync, models.ResourceTypeEnvironment, envID,
		map[string]interface{}{"project_id": p.ID, "added": added})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// DeleteVariable removes an environment variable by ID.
func (h *EnvironmentHandler) DeleteVariable(w http.ResponseWriter, r *http.Request) {
	user := getAuthenticatedUser(w, r)
	if user == nil {
		return
	}

	// For the global /envs/{envId}/variables/{varId} route, verify ownership via envId
	envID := chi.URLParam(r, "envId")
	env := h.verifyEnvOwnership(w, r, user, envID)
	if env == nil {
		return
	}

	varID := chi.URLParam(r, "varId")

	if err := h.projectService.DeleteEnvVariableScoped(r.Context(), varID, envID); err != nil {
		http.Error(w, "failed to delete variable", http.StatusInternalServerError)
		return
	}
	h.audit.record(r, models.AuditActionEnvVarDelete, models.ResourceTypeEnvironment, envID,
		map[string]interface{}{"project_id": env.ProjectID, "variable_id": varID})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}
