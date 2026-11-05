package handlers

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/models"
	pages_services "github.com/raythurman2386/goforge/internal/web/templates/pages/services"
)

// offersBackup reports whether the loaded template declares a strategy the
// nightly run can dump. A missing template or an unknown strategy has no switch.
func (h *ServiceHandler) offersBackup(name string) bool {
	if h.catalog == nil {
		return false
	}
	strategy, ok := h.catalog.BackupStrategy(name)
	return ok && backup.KnownStrategy(strategy)
}

// backupChoice is what the service page's Allows backups card shows.
// The card is omitted when the template has no nightly backup.
func (h *ServiceHandler) backupChoice(inst *models.ServiceInstance) pages_services.BackupChoice {
	return pages_services.BackupChoice{
		InstanceID: inst.ID,
		Enabled:    inst.BackupEnabled,
		Offered:    h.offersBackup(inst.TemplateName),
	}
}

// SetBackupEnabled turns Allows backups on or off
// (POST /services/{id}/backup, form field backup_enabled=true). It only
// changes whether the nightly run and `goforge backup run` include this
// instance. The container is not touched. A template the runner does not
// dump is refused. A change writes service.set_backup (via=ui).
func (h *ServiceHandler) SetBackupEnabled(w http.ResponseWriter, r *http.Request) {
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
	if !h.offersBackup(instance.TemplateName) {
		http.Error(w, "This service template has no nightly backup.", http.StatusConflict)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	// The switch posts "true" when on; HiddenFalse adds "false" either way.
	want := slices.Contains(r.PostForm["backup_enabled"], "true")

	if want != instance.BackupEnabled {
		if err := h.instanceService.SetBackupEnabled(r.Context(), instance.ID, want); err != nil {
			if h.logger != nil {
				h.logger.ErrorContext(r.Context(), "set service backup failed", "service_instance_id", instance.ID, "error", err)
			}
			http.Error(w, "failed to update backups", http.StatusInternalServerError)
			return
		}
		h.audit.record(r, models.AuditActionServiceSetBackup, models.ResourceTypeServiceInstance, instance.ID,
			map[string]interface{}{"name": instance.Name, "backup_enabled": want, "previous": instance.BackupEnabled})
		instance.BackupEnabled = want
	}

	w.Header().Set("Cache-Control", "no-store")
	if err := pages_services.BackupCard(h.backupChoice(instance)).Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render", http.StatusInternalServerError)
	}
}
