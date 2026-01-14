package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/gocool/internal/auth"
	"github.com/raythurman2386/gocool/internal/models"
	"github.com/raythurman2386/gocool/internal/project"
	"github.com/raythurman2386/gocool/internal/web/templates/pages/projects"
)

// ProjectHandler handles project related requests.
type ProjectHandler struct {
	projectService *project.Service
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(ps *project.Service) *ProjectHandler {
	return &ProjectHandler{
		projectService: ps,
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
		http.Error(w, "failed to list projects", http.StatusInternalServerError)
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.List(list, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render projects page", http.StatusInternalServerError)
	}
}

// NewProjectPage renders the form to create a new project.
func (h *ProjectHandler) NewProjectPage(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	component := projects.New(csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render new project page", http.StatusInternalServerError)
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

	created, err := h.projectService.CreateProject(r.Context(), p)
	if err != nil {
		http.Error(w, "failed to create project", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/projects/"+created.ID, http.StatusSeeOther)
}

// GetProject renders the project detail page.
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.projectService.GetProject(r.Context(), id)
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	envs, err := h.projectService.ListEnvironments(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to list environments", http.StatusInternalServerError)
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

	tab := "overview"
	if strings.HasSuffix(r.URL.Path, "/deployments") {
		tab = "deployments"
	} else if strings.HasSuffix(r.URL.Path, "/settings") {
		tab = "settings"
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.Detail(p, envs, currentEnv, tab, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render project detail page", http.StatusInternalServerError)
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
