package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages"
)

// systemStatsCollector is the part of monitor.Collector the dashboard uses.
type systemStatsCollector interface {
	CollectSystemStats(ctx context.Context, userID string) (*models.SystemStats, error)
}

// systemPruner is the part of services.InstanceService Prune uses.
type systemPruner interface {
	PruneSystem(ctx context.Context) (docker.PruneSummary, error)
}

// pruneFailedMessage is the error toast for a failed prune. The Docker error
// is logged, not shown.
const pruneFailedMessage = "Prune System failed. Check the GoForge logs and try again."

// DashboardHandler handles dashboard related requests.
type DashboardHandler struct {
	collector       systemStatsCollector
	instanceService systemPruner
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

// Prune handles Docker system prune requests. An htmx request (the
// dashboard button) gets the re-rendered #dashboard-stats with fresh disk
// numbers plus a success toast out of band, so the page does not reload. A
// failure is a RenderError, which app.js shows as an error toast without
// swapping. A plain form POST still redirects to /dashboard.
func (h *DashboardHandler) Prune(w http.ResponseWriter, r *http.Request) {
	user := requireAdmin(w, r)
	if user == nil {
		return
	}

	summary, err := h.instanceService.PruneSystem(r.Context())
	if err != nil {
		h.logger.Error("system prune failed", "error", err, "space_reclaimed", summary.SpaceReclaimed)
		RenderError(w, r, http.StatusInternalServerError, pruneFailedMessage)
		return
	}
	h.logger.Info("system prune finished",
		"space_reclaimed", summary.SpaceReclaimed,
		"containers", summary.Containers,
		"images", summary.Images,
		"build_cache_records", summary.BuildCacheRecords)

	if r.Header.Get("HX-Request") != htmxTrue {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	stats, err := h.collector.CollectSystemStats(r.Context(), user.ID)
	if err != nil {
		h.logger.Warn("failed to collect system stats after prune", "user_id", user.ID, "error", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx := r.Context()
	if err := pages.DashboardStats(auth.GetCSRFToken(r), stats, user.IsAdmin()).Render(ctx, w); err != nil {
		h.logger.Error("failed to render dashboard stats after prune", "error", err)
		return
	}
	if err := components.Toast(pages.PruneSummaryText(summary), "success").Render(ctx, w); err != nil {
		h.logger.Error("failed to render prune toast", "error", err)
	}
}
