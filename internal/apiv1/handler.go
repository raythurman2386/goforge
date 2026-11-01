package apiv1

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Handler serves /api/v1.
type Handler struct {
	deps      Deps
	protected map[string]bool
	logger    *slog.Logger
	syncWait  time.Duration
	opTimeout time.Duration
}

// New builds the API handler. Mount the result of Routes at /api/v1.
func New(deps Deps) *Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	protected := make(map[string]bool, len(deps.ProtectedServices))
	for _, id := range deps.ProtectedServices {
		if id = strings.TrimSpace(id); id != "" {
			protected[id] = true
		}
	}
	h := &Handler{deps: deps, protected: protected, logger: logger, syncWait: deps.SyncWait, opTimeout: deps.OpTimeout}
	if h.syncWait <= 0 {
		h.syncWait = defaultSyncWait
	}
	if h.opTimeout <= 0 {
		h.opTimeout = defaultOpTimeout
	}
	return h
}

// Routes returns the /api/v1 router. Every route needs a bearer token; reads
// need the read scope and mutations the write scope.
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound, "no such API route")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, codeBadRequest, "method not allowed")
	})
	r.Use(h.requireBearer)

	r.Group(func(r chi.Router) {
		r.Use(requireScope(models.ScopeRead))
		r.Get("/health", h.health)
		r.Get("/whoami", h.whoami)
		r.Get("/dashboard/stats", h.dashboardStats)

		r.Get("/projects", h.listProjects)
		r.Get("/projects/{id}", h.getProject)
		r.Get("/projects/{id}/environments", h.listEnvironments)
		r.Get("/projects/{id}/environments/{envId}/variables", h.listEnvVars)
		r.Get("/projects/{id}/deployments", h.listDeployments)
		r.Get("/projects/{id}/deployments/{deploymentId}", h.getDeployment)
		r.Get("/projects/{id}/deployments/{deploymentId}/logs", h.getDeploymentLogs)

		r.Get("/service-templates", h.listServiceTemplates)
		r.Get("/service-templates/{name}", h.getServiceTemplate)
		r.Get("/services", h.listServices)
		r.Get("/services/{id}", h.getService)

		r.Get("/containers", h.listContainers)
		r.Get("/containers/{id}", h.getContainer)
		r.Get("/containers/{id}/logs", h.getContainerLogs)
		r.Get("/containers/{id}/stats", h.getContainerStats)

		r.Get("/audit-log", h.listAuditLog)
		r.Get("/backups", h.getBackups)

		r.Get("/git-sources", h.listGitSources)
		r.Get("/git-sources/{id}/repos", h.listRepos)
		r.Get("/git-sources/{id}/branches", h.listBranches)
	})

	r.Group(func(r chi.Router) {
		r.Use(requireScope(models.ScopeWrite))
		r.Post("/projects/{id}/deployments", h.triggerDeployment)
		r.Post("/projects/{id}/deployments/{deploymentId}/cancel", h.cancelDeployment)
		r.Post("/projects/{id}/deployments/{deploymentId}/rollback", h.rollbackDeployment)

		r.Post("/projects/{id}/environments/{envId}/variables", h.setEnvVar)
		r.Post("/projects/{id}/environments/{envId}/variables/sync", h.syncEnvVars)
		r.Delete("/projects/{id}/environments/{envId}/variables/{key}", h.deleteEnvVar)

		r.Post("/projects/{id}/services", h.linkService)
		r.Delete("/projects/{id}/services/{serviceId}", h.unlinkService)

		r.Post("/services", h.deployService)
		r.Post("/services/{id}/restart", h.restartService)
		r.Post("/services/{id}/image", h.updateServiceImage)
		r.Patch("/services/{id}/config", h.updateServiceConfig)

		r.Post("/containers/{id}/stop", h.stopContainer)
		r.Post("/containers/{id}/restart", h.restartContainer)
	})

	return r
}

// envProtected reports whether a service instance is listed in
// GOFORGE_PROTECTED_SERVICES.
func (h *Handler) envProtected(instanceID string) bool {
	return instanceID != "" && h.protected[instanceID]
}

// instanceProtected reports whether inst is protected: listed in the env or
// marked protected on its service page.
func (h *Handler) instanceProtected(inst *models.ServiceInstance) bool {
	if inst == nil {
		return false
	}
	return inst.Protected || h.envProtected(inst.ID)
}

// isProtected reports whether the service instance behind instanceID is
// protected (env list or the stored flag). It fails closed: if the instance
// cannot be read for any reason but "not found", it answers true, so a
// database error never lets a stop through on a protected service.
func (h *Handler) isProtected(ctx context.Context, instanceID string) bool {
	if instanceID == "" {
		return false
	}
	if h.envProtected(instanceID) {
		return true
	}
	if h.deps.Instances == nil {
		return false
	}
	inst, err := h.deps.Instances.GetInstance(ctx, instanceID)
	if errors.Is(err, repositories.ErrNotFound) || (err == nil && inst == nil) {
		return false
	}
	if err != nil {
		h.logger.Warn("api v1: protected check could not read the service; treating it as protected", "service_instance_id", instanceID, "error", err)
		return true
	}
	return inst.Protected
}

// protectedList returns the env-listed protected IDs in their configured
// order, then the caller's services marked protected in the UI.
func (h *Handler) protectedList(ctx context.Context, userID string) []string {
	out := make([]string, 0, len(h.deps.ProtectedServices))
	seen := map[string]bool{}
	for _, id := range h.deps.ProtectedServices {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if h.deps.Instances == nil || userID == "" {
		return out
	}
	insts, err := h.deps.Instances.ListInstances(ctx, userID)
	if err != nil {
		h.logger.Warn("api v1: could not list services for the protected list", "error", err)
		return out
	}
	for _, inst := range insts {
		if inst != nil && inst.Protected && !seen[inst.ID] {
			seen[inst.ID] = true
			out = append(out, inst.ID)
		}
	}
	return out
}

func (h *Handler) clientIP(r *http.Request) string {
	if h.deps.ClientIP != nil {
		return h.deps.ClientIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// validIP reports whether s fits the audit_logs.ip_address INET column.
func validIP(s string) bool {
	return net.ParseIP(s) != nil
}
