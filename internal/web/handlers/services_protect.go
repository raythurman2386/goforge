package handlers

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
	pages_services "github.com/raythurman2386/goforge/internal/web/templates/pages/services"
)

// WithEnvProtected passes GOFORGE_PROTECTED_SERVICES to the service page, so
// it shows those services as protected and locks their toggle on.
func (h *ServiceHandler) WithEnvProtected(ids []string) *ServiceHandler {
	h.envProtected = make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			h.envProtected[id] = true
		}
	}
	return h
}

// protection is what the service page's Protected card shows.
func (h *ServiceHandler) protection(inst *models.ServiceInstance) pages_services.Protection {
	return pages_services.Protection{
		InstanceID: inst.ID,
		Stored:     inst.Protected,
		Env:        h.envProtected[inst.ID],
	}
}

// SetProtected turns the stored protected flag of a service on or off
// (POST /services/{id}/protected, form field protected=true). It only
// changes what /api/v1 and MCP tokens may do (stop, config edit, and image
// update need the protected scope); the container is not touched. Services
// listed in GOFORGE_PROTECTED_SERVICES stay protected and cannot be turned
// off here. A change writes service.set_protected (via=ui).
func (h *ServiceHandler) SetProtected(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	instance, err := h.instanceService.GetInstance(r.Context(), id)
	if err != nil || instance == nil {
		http.Error(w, "service instance not found", http.StatusNotFound)
		return
	}
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok || user.ID != instance.UserID {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	// The switch posts "true" when on; HiddenFalse adds "false" either way.
	want := slices.Contains(r.PostForm["protected"], "true")
	if !want && h.envProtected[instance.ID] {
		http.Error(w, "This service is listed in GOFORGE_PROTECTED_SERVICES. Remove it there to unprotect it.", http.StatusConflict)
		return
	}

	if want != instance.Protected {
		if err := h.instanceService.SetProtected(r.Context(), instance.ID, want); err != nil {
			if h.logger != nil {
				h.logger.ErrorContext(r.Context(), "set service protected failed", "service_instance_id", instance.ID, "error", err)
			}
			http.Error(w, "failed to update protection", http.StatusInternalServerError)
			return
		}
		h.audit.record(r, models.AuditActionServiceSetProtected, models.ResourceTypeServiceInstance, instance.ID,
			map[string]interface{}{"name": instance.Name, "protected": want, "previous": instance.Protected})
		instance.Protected = want
	}

	w.Header().Set("Cache-Control", "no-store")
	if err := pages_services.ProtectedCard(h.protection(instance)).Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render", http.StatusInternalServerError)
	}
}
