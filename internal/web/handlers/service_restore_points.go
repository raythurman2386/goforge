package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/models"
	pages_services "github.com/raythurman2386/goforge/internal/web/templates/pages/services"
)

// ServiceRestorePointLister lists one instance's restore points
// (backup.RestorePointLister).
type ServiceRestorePointLister interface {
	ListInstance(ctx context.Context, instanceID string) *backup.InstanceRestorePointsView
}

// WithRestorePoints enables GET /services/{id}/restore-points and the
// Restore points card on service pages (admins, backed-up templates).
func (h *ServiceHandler) WithRestorePoints(l ServiceRestorePointLister) *ServiceHandler {
	h.restorePoints = l
	return h
}

// RestorePoints renders the backup runs that hold this instance's data,
// with the restore-service command (admin and owner only). Read-only:
// restoring is CLI-only.
func (h *ServiceHandler) RestorePoints(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if requireAdmin(w, r) == nil {
		return
	}
	if h.restorePoints == nil {
		http.Error(w, "restore points are not available", http.StatusNotFound)
		return
	}
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
	v := h.restorePoints.ListInstance(r.Context(), instance.ID)
	if v.Error != "" {
		h.logger.Warn("failed to list service restore points", "instance_id", instance.ID, "error", v.Error)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pages_services.RestorePoints(v).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render service restore points", "error", err)
	}
}
