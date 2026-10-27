package handlers

import (
	"log/slog"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/pages"
)

// DashboardHandler handles dashboard related requests.
type DashboardHandler struct {
	collector       *monitor.Collector
	instanceService *services.InstanceService
	projectRepo     *repositories.ProjectRepository
	gitSourceRepo   *repositories.GitSourceRepository
	logger          *slog.Logger
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(collector *monitor.Collector, instanceService *services.InstanceService, projectRepo *repositories.ProjectRepository, gitSourceRepo *repositories.GitSourceRepository, logger *slog.Logger) *DashboardHandler {
	return &DashboardHandler{
		collector:       collector,
		instanceService: instanceService,
		projectRepo:     projectRepo,
		gitSourceRepo:   gitSourceRepo,
		logger:          logger,
	}
}

// Dashboard renders the dashboard page skeleton.
func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	ctx := r.Context()

	// Check if this is a first-time user
	projects, err := h.projectRepo.ListByUserID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to list projects", "error", err)
	}

	gitSources, err := h.gitSourceRepo.ListByUserID(ctx, user.ID)
	if err != nil {
		h.logger.Error("failed to list git sources", "error", err)
	}

	hasProjects := len(projects) > 0
	hasGitSources := len(gitSources) > 0

	csrfToken := auth.GetCSRFToken(r)
	component := pages.Dashboard(csrfToken, hasProjects, hasGitSources)
	if err := component.Render(ctx, w); err != nil {
		http.Error(w, "failed to render dashboard", http.StatusInternalServerError)
	}
}

// DashboardStats returns the HTMX component containing the loaded stats.
func (h *DashboardHandler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	stats, err := h.collector.CollectSystemStats(r.Context(), user.ID)
	if err != nil {
		h.logger.Warn("failed to collect system stats", "user_id", user.ID, "error", err)
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages.DashboardStats(csrfToken, stats, user.IsAdmin())
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render dashboard stats", http.StatusInternalServerError)
	}
}

// Prune handles Docker system prune requests.
func (h *DashboardHandler) Prune(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	if err := h.instanceService.PruneSystem(r.Context()); err != nil {
		http.Error(w, "failed to prune system: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to dashboard
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
