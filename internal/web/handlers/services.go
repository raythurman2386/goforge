package handlers

import (
	"fmt"
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
}

// NewServiceHandler creates a new ServiceHandler.
func NewServiceHandler(c *services.Catalog, is *services.InstanceService) *ServiceHandler {
	return &ServiceHandler{
		catalog:         c,
		instanceService: is,
	}
}

// Catalog renders the service catalog page.
func (h *ServiceHandler) Catalog(w http.ResponseWriter, r *http.Request) {
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
	templateName := chi.URLParam(r, "template")
	tmpl, err := h.catalog.GetTemplate(templateName)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "template not found")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Configure(tmpl, csrfToken)
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
	for key, values := range r.Form {
		if len(values) > 0 {
			config[key] = values[0]
		}
	}

	if _, err := h.instanceService.DeployInstance(r.Context(), user.ID, templateName, config); err != nil {
		RenderError(w, r, http.StatusInternalServerError, fmt.Sprintf("failed to deploy service: %v", err))
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
	component := pages_services.Detail(instance, container, tmpl, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render detail page")
	}
}

// Delete handles service instance deletion.
func (h *ServiceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
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

	if err := h.instanceService.RestartInstance(r.Context(), id); err != nil {
		http.Error(w, "failed to restart service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
