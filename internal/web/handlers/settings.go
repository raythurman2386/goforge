package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/layouts"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

// SettingsHandler handles settings related requests.
type SettingsHandler struct {
	gitSourceRepo   *repositories.GitSourceRepository
	instanceService *services.InstanceService
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(gsr *repositories.GitSourceRepository, is *services.InstanceService) *SettingsHandler {
	return &SettingsHandler{
		gitSourceRepo:   gsr,
		instanceService: is,
	}
}

// GitSources renders the git sources settings page.
func (h *SettingsHandler) GitSources(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	sources, err := h.gitSourceRepo.ListByUserID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to list git sources")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := settings.GitSources(sources, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to render settings page")
	}
}

// NewGitSourceModal renders the modal to add a new git source.
func (h *SettingsHandler) NewGitSourceModal(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	component := settings.AddGitSourceModal(csrfToken)

	if r.Header.Get("HX-Request") == "true" {
		if err := component.Render(r.Context(), w); err != nil {
			http.Error(w, "failed to render modal", http.StatusInternalServerError)
		}
		return
	}

	// For direct access, wrap in AppLayout
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	layout := layouts.AppLayout("Add Git Source", csrfToken)
	if err := layout.Render(templ.WithChildren(r.Context(), component), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

// CreateGitSource handles creating a new git source.
func (h *SettingsHandler) CreateGitSource(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	name := r.FormValue("name")
	sourceType := r.FormValue("type")
	apiURL := r.FormValue("api_url")
	accessToken := r.FormValue("access_token")

	source := &models.GitSource{
		UserID:      user.ID,
		Name:        name,
		Type:        models.GitSourceType(sourceType),
		AccessToken: sql.NullString{String: accessToken, Valid: accessToken != ""},
	}
	if apiURL != "" {
		source.APIURL = sql.NullString{String: apiURL, Valid: true}
	}

	if err := h.gitSourceRepo.Create(r.Context(), source); err != nil {
		http.Error(w, "failed to create git source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings/git")
	w.WriteHeader(http.StatusCreated)
}

// DeleteGitSource handles deleting a git source.
func (h *SettingsHandler) DeleteGitSource(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.gitSourceRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete git source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings/git")
	w.WriteHeader(http.StatusOK)
}

// PruneSystem triggers a system prune.
func (h *SettingsHandler) PruneSystem(w http.ResponseWriter, r *http.Request) {
	if err := h.instanceService.PruneSystem(r.Context()); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to prune system")
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := components.Toast("System pruned successfully", "success").Render(r.Context(), w); err != nil {
		slog.Error("failed to render prune toast", "error", err)
	}
}
