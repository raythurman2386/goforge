package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/projects"
)

// Tab name constants.
const (
	tabOverview    = "overview"
	tabDeployments = "deployments"
	tabSettings    = "settings"
	tabServices    = "services"
)

// ProjectHandler handles project related requests.
type ProjectHandler struct {
	projectService         *project.Service
	gitSourceRepo          *repositories.GitSourceRepository
	deploymentRepo         *repositories.DeploymentRepository
	containerRepo          *repositories.ContainerRepository
	projectServiceInstRepo *repositories.ProjectServiceInstanceRepository
	instanceService        *services.InstanceService
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(
	ps *project.Service,
	gsr *repositories.GitSourceRepository,
	dr *repositories.DeploymentRepository,
	cr *repositories.ContainerRepository,
	psir *repositories.ProjectServiceInstanceRepository,
	is *services.InstanceService,
) *ProjectHandler {
	return &ProjectHandler{
		projectService:         ps,
		gitSourceRepo:          gsr,
		deploymentRepo:         dr,
		containerRepo:          cr,
		projectServiceInstRepo: psir,
		instanceService:        is,
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

	var instances []*models.ServiceInstance
	if h.instanceService != nil {
		var err error
		instances, err = h.instanceService.ListInstances(r.Context(), user.ID)
		if err != nil {
			slog.Warn("failed to list service instances for project creation", "error", err)
			instances = nil
		}
	}

	// Filter to only running instances
	var runningInstances []*models.ServiceInstance
	for _, inst := range instances {
		if inst.Status == models.ServiceStatusRunning {
			runningInstances = append(runningInstances, inst)
		}
	}

	csrfToken := auth.GetCSRFToken(r)
	component := projects.New(sources, runningInstances, csrfToken)
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

	// Link selected service instances
	if h.projectServiceInstRepo != nil {
		if err := r.ParseForm(); err == nil {
			serviceIDs := r.Form["service_ids"]
			for _, svcID := range serviceIDs {
				if svcID == "" {
					continue
				}
				if linkErr := h.projectServiceInstRepo.Link(r.Context(), created.ID, svcID); linkErr != nil {
					slog.Warn("failed to link service to project", "project_id", created.ID, "service_id", svcID, "error", linkErr)
				}
			}
		}
	}

	http.Redirect(w, r, "/projects/"+created.ID, http.StatusSeeOther)
}

// GetProject renders the project detail page.
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

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

	tab := resolveTab(r.URL.Path)

	var deployments []*models.Deployment
	if tab == tabDeployments && currentEnv != nil {
		deployments, err = h.deploymentRepo.ListByEnvironmentID(r.Context(), currentEnv.ID, 50)
		if err != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to list deployments")
			return
		}
	}

	linkedServices, availableServices := h.fetchServiceLinks(r.Context(), id, user.ID, tab)

	csrfToken := auth.GetCSRFToken(r)
	component := projects.Detail(p, envs, currentEnv, latestDeployment, deployments, activeContainer, linkedServices, availableServices, tab, csrfToken)
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

// resolveTab determines the active tab from the URL path suffix.
func resolveTab(urlPath string) string {
	switch path.Base(urlPath) {
	case tabDeployments:
		return tabDeployments
	case tabSettings:
		return tabSettings
	case tabServices:
		return tabServices
	default:
		return tabOverview
	}
}

// fetchServiceLinks retrieves linked and available (unlinked, running) service instances for a project.
func (h *ProjectHandler) fetchServiceLinks(ctx context.Context, projectID, userID, tab string) (linked, available []*models.ServiceInstance) {
	if h.projectServiceInstRepo != nil {
		var err error
		linked, err = h.projectServiceInstRepo.ListByProjectID(ctx, projectID)
		if err != nil {
			slog.Warn("failed to list linked services", "project_id", projectID, "error", err)
		}
	}

	if tab == tabServices && h.instanceService != nil {
		allInstances, err := h.instanceService.ListInstances(ctx, userID)
		if err != nil {
			slog.Warn("failed to list service instances", "error", err)
		} else {
			linkedIDs := make(map[string]bool, len(linked))
			for _, ls := range linked {
				linkedIDs[ls.ID] = true
			}
			for _, inst := range allInstances {
				if inst.Status == models.ServiceStatusRunning && !linkedIDs[inst.ID] {
					available = append(available, inst)
				}
			}
		}
	}

	return linked, available
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

// LinkService associates a service instance with a project.
func (h *ProjectHandler) LinkService(w http.ResponseWriter, r *http.Request) {
	if h.projectServiceInstRepo == nil {
		http.Error(w, "service linking not available", http.StatusServiceUnavailable)
		return
	}

	projectID := chi.URLParam(r, "id")
	serviceID := r.FormValue("service_instance_id")
	if serviceID == "" {
		http.Error(w, "service_instance_id is required", http.StatusBadRequest)
		return
	}

	if err := h.projectServiceInstRepo.Link(r.Context(), projectID, serviceID); err != nil {
		http.Error(w, "failed to link service", http.StatusInternalServerError)
		return
	}

	// Best-effort: inject conventional env vars for the linked service into the
	// project's first environment. Errors here are non-fatal.
	if h.instanceService != nil {
		h.injectServiceEnvVars(r.Context(), projectID, serviceID)
	}

	w.Header().Set("HX-Redirect", "/projects/"+projectID+"/services")
	w.WriteHeader(http.StatusOK)
}

// injectServiceEnvVars inserts conventional connection-string env vars for a newly linked
// service instance into the project's first environment. Existing keys are skipped.
func (h *ProjectHandler) injectServiceEnvVars(ctx context.Context, projectID, serviceID string) {
	inst, err := h.instanceService.GetInstance(ctx, serviceID)
	if err != nil {
		slog.Warn("injectServiceEnvVars: could not get instance", "service_id", serviceID, "error", err)
		return
	}

	connInfo, err := inst.GetConnectionInfo()
	if err != nil || len(connInfo) == 0 {
		return
	}

	envs, err := h.projectService.ListEnvironments(ctx, projectID)
	if err != nil || len(envs) == 0 {
		return
	}
	env := envs[0]

	// Collect existing keys so we don't overwrite them.
	existing, err := h.projectService.ListEnvVariables(ctx, env.ID)
	if err != nil {
		return
	}
	existingKeys := make(map[string]bool, len(existing))
	for _, v := range existing {
		existingKeys[v.Key] = true
	}

	for connKey, connVal := range connInfo {
		varName := services.ServiceEnvVarName(inst.Name, inst.TemplateName, connKey)
		if existingKeys[varName] {
			continue
		}
		v := &models.EnvVariable{
			EnvironmentID: env.ID,
			Key:           varName,
			Value:         connVal,
			IsSecret:      true,
		}
		if addErr := h.projectService.AddEnvVariable(ctx, v); addErr != nil {
			slog.Warn("injectServiceEnvVars: failed to add env var", "key", varName, "error", addErr)
		}
	}
}

// UnlinkService removes the association between a service instance and a project.
func (h *ProjectHandler) UnlinkService(w http.ResponseWriter, r *http.Request) {
	if h.projectServiceInstRepo == nil {
		http.Error(w, "service linking not available", http.StatusServiceUnavailable)
		return
	}

	projectID := chi.URLParam(r, "id")
	serviceID := chi.URLParam(r, "serviceId")

	if err := h.projectServiceInstRepo.Unlink(r.Context(), projectID, serviceID); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			http.Error(w, "service link not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to unlink service", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/projects/"+projectID+"/services")
	w.WriteHeader(http.StatusOK)
}
