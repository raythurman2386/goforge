package handlers

import (
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/web/templates/pages"
)

// DashboardHandler handles dashboard related requests.
type DashboardHandler struct {
	collector *monitor.Collector
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(collector *monitor.Collector) *DashboardHandler {
	return &DashboardHandler{
		collector: collector,
	}
}

// Dashboard renders the dashboard page.
func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	stats, err := h.collector.CollectSystemStats(r.Context(), user.ID)
	if err != nil {
		// Just log the error, don't fail the request.
		// In a real logger we would use the logger instance.
		// For now we just proceed with nil stats.
		_ = err
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages.Dashboard(csrfToken, stats)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render dashboard", http.StatusInternalServerError)
	}
}
