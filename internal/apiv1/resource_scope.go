package apiv1

import (
	"context"
	"net/http"

	"github.com/raythurman2386/goforge/internal/models"
)

// Resource scoping (migration 027). A token with project_ids and/or
// service_ids is restricted: it may only touch those projects (with their
// environments, variables, deployments, links, and deployment containers)
// and those services (with their containers). Anything else answers 403
// resource_out_of_scope. The check runs after ownership and before any
// dry-run plan, audit row, or Docker call. A token with both lists empty is
// host-wide, the behavior before slice 15. Scopes are unchanged: read,
// write, and protected still gate the action type.

// outOfScope answers 403 resource_out_of_scope.
func outOfScope(w http.ResponseWriter, what string) {
	writeError(w, http.StatusForbidden, codeOutOfScope, "this token is restricted to listed projects and services; "+what+" is not one of them")
}

// requireHostWide refuses restricted tokens on routes that are not about a
// single project or service: host stats, backups, git sources, and creating
// a new service (its id cannot be on the token's list yet).
func requireHostWide(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, token := caller(r)
		if token == nil || token.IsRestricted() {
			outOfScope(w, "this host-wide route")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowsProject and allowsService read the caller's token. No token (never
// the case behind requireBearer) allows nothing.
func allowsProject(r *http.Request, id string) bool {
	_, token := caller(r)
	return token != nil && token.AllowsProject(id)
}

func allowsService(r *http.Request, id string) bool {
	_, token := caller(r)
	return token != nil && token.AllowsService(id)
}

// containerInScope reports whether token may touch container c: a service
// container needs its service on the list, a deployment container needs its
// project on the list. It fails closed: an untracked container, or one whose
// deployment or environment cannot be read, is out of scope for a
// restricted token.
func (h *Handler) containerInScope(ctx context.Context, token *models.APIToken, c *models.Container) bool {
	if token == nil {
		return false
	}
	if !token.IsRestricted() {
		return true
	}
	if c == nil {
		return false
	}
	if instanceID := containerInstanceID(c); instanceID != "" {
		return token.AllowsService(instanceID)
	}
	if !c.DeploymentID.Valid || c.DeploymentID.String == "" || h.deps.Deployments == nil || h.deps.Projects == nil {
		return false
	}
	d, err := h.deps.Deployments.GetDeployment(ctx, c.DeploymentID.String)
	if err != nil || d == nil {
		return false
	}
	env, err := h.deps.Projects.GetEnvironment(ctx, d.EnvironmentID)
	if err != nil || env == nil {
		return false
	}
	return token.AllowsProject(env.ProjectID)
}

// resourceScopeDTO is the restriction whoami reports. The lists are never
// null so agents can test them without a nil check.
type resourceScopeDTO struct {
	Restricted bool     `json:"restricted"`
	ProjectIDs []string `json:"project_ids"`
	ServiceIDs []string `json:"service_ids"`
}

func toResourceScope(t *models.APIToken) resourceScopeDTO {
	out := resourceScopeDTO{Restricted: t.IsRestricted(), ProjectIDs: []string{}, ServiceIDs: []string{}}
	out.ProjectIDs = append(out.ProjectIDs, t.ProjectIDs...)
	out.ServiceIDs = append(out.ServiceIDs, t.ServiceIDs...)
	return out
}
