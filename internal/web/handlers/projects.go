package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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
	containerRepo  *repositories.ContainerRepository
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(ps *project.Service, gsr *repositories.GitSourceRepository, dr *repositories.DeploymentRepository, cr *repositories.ContainerRepository) *ProjectHandler {
	return &ProjectHandler{
		projectService: ps,
		gitSourceRepo:  gsr,
		deploymentRepo: dr,
		containerRepo:  cr,
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

	currentEnv := h.getCurrentEnv(envs, r.URL.Query().Get("env"))

	var latestDeployment *models.Deployment
	var activeContainer *models.Container

	if currentEnv != nil {
		latestDeployment, err = h.deploymentRepo.GetLatestByEnvironmentID(r.Context(), currentEnv.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("failed to fetch latest deployment", "environment_id", currentEnv.ID, "error", err)
		}
		if err == nil && latestDeployment != nil && h.containerRepo != nil {
			activeContainer = h.getActiveContainer(r.Context(), latestDeployment.ID)
		}
	}

	tab := "overview"
	if strings.HasSuffix(r.URL.Path, "/deployments") {
		tab = "deployments"
	} else if strings.HasSuffix(r.URL.Path, "/settings") {
		tab = "settings"
	}

	var deployments []*models.Deployment
	if tab == "deployments" && currentEnv != nil {
		deployments, err = h.deploymentRepo.ListByEnvironmentID(r.Context(), currentEnv.ID, 50)
		if err != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to list deployments")
			return
		}
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.Detail(p, envs, currentEnv, latestDeployment, deployments, activeContainer, tab, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render project detail page")
	}
}

func (h *ProjectHandler) getCurrentEnv(envs []*models.Environment, envID string) *models.Environment {
	if len(envs) == 0 {
		return nil
	}
	if envID != "" {
		for _, e := range envs {
			if e.ID == envID {
				return e
			}
		}
	}
	return envs[0]
}

func (h *ProjectHandler) getActiveContainer(ctx context.Context, deploymentID string) *models.Container {
	containers, err := h.containerRepo.ListByDeploymentID(ctx, deploymentID)
	if err != nil || len(containers) == 0 {
		return nil
	}
	for _, c := range containers {
		if c.Status == models.ContainerStatusRunning {
			return c
		}
	}
	return containers[0]
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

// UpdateSettings handles unified updates for project and environment settings.
func (h *ProjectHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	envID := r.FormValue("env_id")

	// Update Project
	p, err := h.projectService.GetProject(r.Context(), projectID)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "project not found")
		return
	}

	customDomain := r.FormValue("custom_domain")
	// Only update if present in form (it will be, but safe check)
	p.CustomDomain = sql.NullString{String: customDomain, Valid: customDomain != ""}

	if err := h.projectService.UpdateProject(r.Context(), p); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to update project settings")
		return
	}

	// Update Environment if ID provided
	if envID != "" {
		env, err := h.projectService.GetEnvironment(r.Context(), envID)
		if err != nil {
			RenderError(w, r, http.StatusNotFound, "environment not found")
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
			RenderError(w, r, http.StatusInternalServerError, "failed to update environment settings")
			return
		}
	}

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}
