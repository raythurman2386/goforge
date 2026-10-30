package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
	pages_services "github.com/raythurman2386/goforge/internal/web/templates/pages/services"
)

// ServiceHandler handles service catalog requests.
type ServiceHandler struct {
	catalog         *services.Catalog
	instanceService *services.InstanceService
	inspector       containerInspector
	logger          *slog.Logger
}

// WithInspector lets service detail show the Container card from Docker
// inspect. Without it the card says details are unavailable.
func (h *ServiceHandler) WithInspector(insp containerInspector) *ServiceHandler {
	h.inspector = insp
	return h
}

// NewServiceHandler creates a new ServiceHandler.
func NewServiceHandler(c *services.Catalog, is *services.InstanceService, logger *slog.Logger) *ServiceHandler {
	return &ServiceHandler{
		catalog:         c,
		instanceService: is,
		logger:          logger,
	}
}

// refreshCatalog picks up templates added, edited, or removed under
// TEMPLATES_DIR since the last load, so new templates do not need an image
// rebuild or restart. A directory-read failure keeps the last catalog;
// a file that fails to parse is skipped (that template drops until fixed).
func (h *ServiceHandler) refreshCatalog(r *http.Request) {
	reloaded, err := h.catalog.ReloadIfChanged()
	if err != nil && h.logger != nil {
		h.logger.WarnContext(r.Context(), "service catalog reload had errors", "dir", h.catalog.Dir(), "error", err)
	}
	if reloaded && h.logger != nil {
		h.logger.InfoContext(r.Context(), "service catalog reloaded", "dir", h.catalog.Dir(), "count", h.catalog.Count())
	}
}

// Catalog renders the service catalog page.
func (h *ServiceHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	h.refreshCatalog(r)
	templates := h.catalog.ListTemplates()

	// Also get running instances
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	instances, err := h.instanceService.ListInstances(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to list instances")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Index(templates, instances, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render catalog")
	}
}

// Configure renders the configuration page for a service template.
func (h *ServiceHandler) Configure(w http.ResponseWriter, r *http.Request) {
	h.refreshCatalog(r)
	templateName := chi.URLParam(r, "template")
	tmpl, err := h.catalog.GetTemplate(templateName)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "template not found")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Configure(tmpl, csrfToken, h.instanceService.CloudflareProxyMode())
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render configuration page")
	}
}

// Deploy handles service deployment.
func (h *ServiceHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	templateName := chi.URLParam(r, "template")
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse form data into config map
	config := make(map[string]interface{})
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "invalid form data")
		return
	}
	// Filter out non-config form fields (CSRF token, name, domain are handled separately)
	skipFields := map[string]bool{
		"csrf_token":         true,
		"gorilla.csrf.Token": true,
	}
	for key, values := range r.Form {
		if len(values) > 0 && !skipFields[key] {
			config[key] = values[0]
		}
	}

	if _, err := h.instanceService.DeployInstance(r.Context(), user.ID, templateName, config); err != nil {
		if errors.Is(err, services.ErrInvalidConfig) {
			RenderError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		h.logger.Error("failed to deploy service", "template", templateName, "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to deploy service")
		return
	}

	http.Redirect(w, r, "/services", http.StatusSeeOther)
}

// Show renders the service instance detail page.
func (h *ServiceHandler) Show(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "service instance not found")
		return
	}

	// Verify ownership
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok || user.ID != instance.UserID {
		RenderError(w, r, http.StatusForbidden, "access denied")
		return
	}

	container, err := h.instanceService.GetContainer(r.Context(), id)
	if err != nil {
		// Log error but continue - container might not exist yet
		container = nil
	}

	tmpl, err := h.catalog.GetTemplate(instance.TemplateName)
	if err != nil {
		// Template might have been removed, handled gracefully in UI
		tmpl = &services.ServiceTemplate{DisplayName: instance.TemplateName}
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Detail(instance, container, inspectSummary(r.Context(), h.inspector, container, h.logger), tmpl, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render detail page")
	}
}

// RevealSecret returns one masked Connection Details value as plain text for
// the page's Show and Copy buttons, so secrets are not in the page HTML.
func (h *ServiceHandler) RevealSecret(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		http.Error(w, "service instance not found", http.StatusNotFound)
		return
	}
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok || user.ID != instance.UserID {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	tmpl, err := h.catalog.GetTemplate(instance.TemplateName)
	if err != nil {
		tmpl = nil
	}
	value, found := services.SecretValue(tmpl, instance, r.URL.Query().Get("source"), r.URL.Query().Get("key"))
	if !found {
		http.Error(w, "secret not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write([]byte(value)); err != nil {
		h.logger.Warn("failed to write secret response", "instance_id", id, "error", err)
	}
}

// EditConfig renders the edit form for a deployed service instance,
// prefilled from its stored config. Password fields start blank.
func (h *ServiceHandler) EditConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "service instance not found")
		return
	}
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok || user.ID != instance.UserID {
		RenderError(w, r, http.StatusForbidden, "access denied")
		return
	}

	tmpl, err := h.catalog.GetTemplate(instance.TemplateName)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "service template not found; this service cannot be edited")
		return
	}

	config, err := instance.GetConfig()
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to read service config")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Edit(instance, tmpl, config, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render edit page")
	}
}

// UpdateConfig saves edited config and recreates the service container.
// The instance keeps its ID, so the same named volumes are mounted again.
func (h *ServiceHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "service instance not found")
		return
	}
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok || user.ID != instance.UserID {
		RenderError(w, r, http.StatusForbidden, "access denied")
		return
	}

	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "invalid form data")
		return
	}
	// The service keeps only name, domain, and template fields from this map.
	updates := make(map[string]interface{}, len(r.PostForm))
	for key, values := range r.PostForm {
		if len(values) > 0 {
			updates[key] = values[0]
		}
	}

	if _, err := h.instanceService.UpdateInstanceConfig(r.Context(), id, updates); err != nil {
		if errors.Is(err, services.ErrInstanceBusy) {
			RenderError(w, r, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, services.ErrInvalidConfig) {
			RenderError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		h.logger.Error("failed to update service config", "instance_id", id, "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to update service configuration")
		return
	}

	http.Redirect(w, r, "/services/"+id, http.StatusSeeOther)
}

// UpdateImage pulls the service's image again and recreates its container
// with the same config and named volumes. It is the way to pick up a newer
// upstream image on a moving tag such as latest.
func (h *ServiceHandler) UpdateImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		http.Error(w, "service instance not found", http.StatusNotFound)
		return
	}
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok || user.ID != instance.UserID {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	if _, err := h.instanceService.UpdateInstanceImage(r.Context(), id); err != nil {
		if errors.Is(err, services.ErrInstanceBusy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		h.logger.Error("failed to update service image", "instance_id", id, "error", err)
		http.Error(w, "failed to update service", http.StatusInternalServerError)
		return
	}

	// Reload the detail page so it shows the pending state and starts polling.
	w.Header().Set("HX-Redirect", "/services/"+id)
	w.WriteHeader(http.StatusOK)
}

// Delete handles service instance deletion.
func (h *ServiceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Verify ownership
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		http.Error(w, "service instance not found", http.StatusNotFound)
		return
	}
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok || user.ID != instance.UserID {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	if err := h.instanceService.DeleteInstance(r.Context(), id); err != nil {
		http.Error(w, "failed to delete service instance", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/services")
	w.WriteHeader(http.StatusOK)
}

// Restart handles service instance restart.
func (h *ServiceHandler) Restart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "instance ID is required", http.StatusBadRequest)
		return
	}

	// Verify ownership
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		http.Error(w, "service instance not found", http.StatusNotFound)
		return
	}
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok || user.ID != instance.UserID {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	if err := h.instanceService.RestartInstance(r.Context(), id); err != nil {
		http.Error(w, "failed to restart service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// InstanceList renders the instance grid fragment for HTMX polling.
func (h *ServiceHandler) InstanceList(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	instances, err := h.instanceService.ListInstances(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "failed to list instances", http.StatusInternalServerError)
		return
	}

	component := pages_services.InstanceList(instances)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render instance list", http.StatusInternalServerError)
	}
}
