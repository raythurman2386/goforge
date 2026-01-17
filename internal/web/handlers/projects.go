package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/projects"
)

// ProjectHandler handles project related requests.
type ProjectHandler struct {
	projectService *project.Service
	gitSourceRepo  *repositories.GitSourceRepository
	deploymentRepo *repositories.DeploymentRepository
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(ps *project.Service, gsr *repositories.GitSourceRepository, dr *repositories.DeploymentRepository) *ProjectHandler {
	return &ProjectHandler{
		projectService: ps,
		gitSourceRepo:  gsr,
		deploymentRepo: dr,
	}
}

// ListProjects renders the project list page.
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	list, err := h.projectService.ListProjects(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to list projects")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.List(list, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render projects page")
	}
}

// NewProjectPage renders the form to create a new project.
func (h *ProjectHandler) NewProjectPage(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	sources, err := h.gitSourceRepo.ListByUserID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to list git sources")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.New(sources, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render new project page")
	}
}

// CreateProject handles project creation.
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	repoURL := r.FormValue("repository_url")
	gitSourceID := r.FormValue("git_source_id")
	defaultBranch := r.FormValue("default_branch")
	dockerfilePath := r.FormValue("dockerfile_path")
	buildContext := r.FormValue("build_context")

	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}
	if buildContext == "" {
		buildContext = "."
	}

	p := &models.Project{
		UserID:         user.ID,
		Name:           name,
		RepositoryURL:  repoURL,
		DefaultBranch:  defaultBranch,
		DockerfilePath: dockerfilePath,
		BuildContext:   buildContext,
	}

	// Set GitSourceID if provided
	if gitSourceID != "" {
		p.GitSourceID = sql.NullString{String: gitSourceID, Valid: true}
	}

	created, err := h.projectService.CreateProject(r.Context(), p)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to create project")
		return
	}

	http.Redirect(w, r, "/projects/"+created.ID, http.StatusSeeOther)
}

// GetProject renders the project detail page.
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.projectService.GetProject(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "project not found")
		return
	}

	envs, err := h.projectService.ListEnvironments(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to list environments")
		return
	}

	// Determine current environment
	envID := r.URL.Query().Get("env")
	var currentEnv *models.Environment
	if len(envs) > 0 {
		if envID != "" {
			for _, e := range envs {
				if e.ID == envID {
					currentEnv = e
					break
				}
			}
		}
		// If explicit env not found or not provided, default to first (usually production)
		if currentEnv == nil {
			currentEnv = envs[0]
		}
	}

	// Get latest deployment for the current environment
	var latestDeployment *models.Deployment
	if currentEnv != nil {
		// Ignore error - it's ok if there's no deployment yet
		latestDeployment, err = h.deploymentRepo.GetLatestByEnvironmentID(r.Context(), currentEnv.ID)
		if err != nil {
			latestDeployment = nil
		}
	}

	tab := "overview"
	if strings.HasSuffix(r.URL.Path, "/deployments") {
		tab = "deployments"
	} else if strings.HasSuffix(r.URL.Path, "/settings") {
		tab = "settings"
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.Detail(p, envs, currentEnv, latestDeployment, tab, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render project detail page")
	}
}

// DeleteProject handles project deletion.
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.projectService.DeleteProject(r.Context(), id); err != nil {
		http.Error(w, "failed to delete project", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/projects")
	w.WriteHeader(http.StatusOK)
}

// UpdateProject handles project updates.
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.projectService.GetProject(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "project not found")
		return
	}

	customDomain := r.FormValue("custom_domain")
	p.CustomDomain = sql.NullString{String: customDomain, Valid: customDomain != ""}

	if err := h.projectService.UpdateProject(r.Context(), p); err != nil {
		http.Error(w, "failed to update project", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
