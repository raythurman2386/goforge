package handlers

import (
	"net/http"

	"github.com/raythurman2386/gocool/internal/auth"
	"github.com/raythurman2386/gocool/internal/web/templates/pages"
)

// DashboardHandler handles dashboard related requests.
type DashboardHandler struct {
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler() *DashboardHandler {
	return &DashboardHandler{}
}

// Dashboard renders the dashboard page.
func (h *DashboardHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	component := pages.Dashboard(csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render dashboard", http.StatusInternalServerError)
	}
}
