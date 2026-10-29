package apiv1

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/models"
)

// Ownership answers match the HTML routes after #28: another user's project,
// environment, or deployment is 404; another user's service instance,
// container, or git source is 403.

func (h *Handler) loadProject(w http.ResponseWriter, r *http.Request) (*models.Project, bool) {
	user, _ := caller(r)
	if h.deps.Projects == nil {
		unavailable(w, "projects")
		return nil, false
	}
	p, err := h.deps.Projects.GetProject(r.Context(), chi.URLParam(r, "id"))
	if err != nil || p == nil || user == nil || p.UserID != user.ID {
		notFound(w, "project")
		return nil, false
	}
	return p, true
}

func (h *Handler) loadEnvironment(w http.ResponseWriter, r *http.Request, p *models.Project, envID string) (*models.Environment, bool) {
	env, err := h.deps.Projects.GetEnvironment(r.Context(), envID)
	if err != nil || env == nil || env.ProjectID != p.ID {
		notFound(w, "environment")
		return nil, false
	}
	return env, true
}

func (h *Handler) loadDeployment(w http.ResponseWriter, r *http.Request, p *models.Project) (*models.Deployment, *models.Environment, bool) {
	if h.deps.Deployments == nil {
		unavailable(w, "deployments")
		return nil, nil, false
	}
	d, err := h.deps.Deployments.GetDeployment(r.Context(), chi.URLParam(r, "deploymentId"))
	if err != nil || d == nil {
		notFound(w, "deployment")
		return nil, nil, false
	}
	env, err := h.deps.Projects.GetEnvironment(r.Context(), d.EnvironmentID)
	if err != nil || env == nil || env.ProjectID != p.ID {
		notFound(w, "deployment")
		return nil, nil, false
	}
	return d, env, true
}

func (h *Handler) loadInstance(w http.ResponseWriter, r *http.Request, id string) (*models.ServiceInstance, bool) {
	user, _ := caller(r)
	if h.deps.Instances == nil {
		unavailable(w, "services")
		return nil, false
	}
	inst, err := h.deps.Instances.GetInstance(r.Context(), id)
	if err != nil || inst == nil {
		notFound(w, "service instance")
		return nil, false
	}
	if user == nil || inst.UserID != user.ID {
		forbidden(w, "access denied")
		return nil, false
	}
	return inst, true
}

// loadContainer checks ownership through the containers table. An untracked
// or foreign container is 403, the same as the HTML stop and restart routes.
// The returned row is nil only if it vanished between the two queries.
func (h *Handler) loadContainer(w http.ResponseWriter, r *http.Request) (string, *models.Container, bool) {
	user, _ := caller(r)
	if h.deps.Containers == nil {
		unavailable(w, "containers")
		return "", nil, false
	}
	dockerID := chi.URLParam(r, "id")
	owned, err := h.deps.Containers.OwnedByUser(r.Context(), dockerID, user.ID)
	if err != nil || !owned {
		forbidden(w, "access denied")
		return "", nil, false
	}
	c, err := h.deps.Containers.GetByDockerID(r.Context(), dockerID)
	if err != nil {
		c = nil
	}
	return dockerID, c, true
}

func (h *Handler) loadGitSource(w http.ResponseWriter, r *http.Request) (*models.GitSource, bool) {
	user, _ := caller(r)
	if h.deps.GitSources == nil {
		unavailable(w, "git sources")
		return nil, false
	}
	src, err := h.deps.GitSources.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil || src == nil {
		notFound(w, "git source")
		return nil, false
	}
	if src.UserID != user.ID {
		forbidden(w, "access denied")
		return nil, false
	}
	return src, true
}

// containerInstanceID returns the service instance behind a tracked
// container, from the row or from the goforge-service-<id> name.
func containerInstanceID(c *models.Container) string {
	if c == nil {
		return ""
	}
	if c.ServiceInstanceID.Valid && c.ServiceInstanceID.String != "" {
		return c.ServiceInstanceID.String
	}
	const prefix = "goforge-service-"
	name := c.Name
	if name != "" && name[0] == '/' {
		name = name[1:]
	}
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		return name[len(prefix):]
	}
	return ""
}
