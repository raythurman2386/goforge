package handlers

import (
	"log/slog"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/pages"
)

// DashboardHandler handles dashboard related requests.
type DashboardHandler struct {
	collector       *monitor.Collector
	instanceService *services.InstanceService
	logger          *slog.Logger
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(collector *monitor.Collector, instanceService *services.InstanceService, logger *slog.Logger) *DashboardHandler {
	return &DashboardHandler{
		collector:       collector,
		instanceService: instanceService,
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

	csrfToken := auth.GetCSRFToken(r)
	component := pages.Dashboard(csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
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
	component := pages.DashboardStats(csrfToken, stats)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render dashboard stats", http.StatusInternalServerError)
	}
}

// Prune handles Docker system prune requests.
func (h *DashboardHandler) Prune(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if err := h.instanceService.PruneSystem(r.Context()); err != nil {
		http.Error(w, "failed to prune system: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect back to dashboard
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
