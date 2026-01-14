package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/gocool/internal/auth"
	"github.com/raythurman2386/gocool/internal/models"
	"github.com/raythurman2386/gocool/internal/services"
	pages_services "github.com/raythurman2386/gocool/internal/web/templates/pages/services"
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
		http.Error(w, "failed to list instances", http.StatusInternalServerError)
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Index(templates, instances, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render catalog", http.StatusInternalServerError)
	}
}

// Configure renders the configuration page for a service template.
func (h *ServiceHandler) Configure(w http.ResponseWriter, r *http.Request) {
	templateName := chi.URLParam(r, "template")
	tmpl, err := h.catalog.GetTemplate(templateName)
	if err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Configure(tmpl, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render configuration page", http.StatusInternalServerError)
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
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	for key, values := range r.Form {
		if len(values) > 0 {
			config[key] = values[0]
		}
	}

	if _, err := h.instanceService.DeployInstance(r.Context(), user.ID, templateName, config); err != nil {
		http.Error(w, fmt.Sprintf("failed to deploy service: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/services", http.StatusSeeOther)
}

// Show renders the service instance detail page.
func (h *ServiceHandler) Show(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil {
		http.Error(w, "service instance not found", http.StatusNotFound)
		return
	}

	tmpl, err := h.catalog.GetTemplate(instance.TemplateName)
	if err != nil {
		// Template might have been removed, handled gracefully in UI
		tmpl = &services.ServiceTemplate{DisplayName: instance.TemplateName}
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages_services.Detail(instance, tmpl, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render detail page", http.StatusInternalServerError)
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
	// TODO: Implement actual restart logic in service
	w.WriteHeader(http.StatusOK)
}
