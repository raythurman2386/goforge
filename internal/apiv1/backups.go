package apiv1

import "net/http"

// getBackups returns the backup status: whether backups are on, the
// schedule, the next run, and the recent backup_runs rows. Admin only, since
// it describes the whole instance. Read-only: v1 has no run-now endpoint.
// Credentials and the endpoint are never included.
func (h *Handler) getBackups(w http.ResponseWriter, r *http.Request) {
	user, _ := caller(r)
	if !user.IsAdmin() {
		forbidden(w, "backup status requires an admin account")
		return
	}
	if h.deps.Backups == nil {
		unavailable(w, "backup status")
		return
	}
	st, err := h.deps.Backups.Status(r.Context())
	if err != nil {
		h.logger.Error("failed to read backup status", "error", err)
		internalError(w, "failed to read backup status")
		return
	}
	writeJSON(w, http.StatusOK, st)
}
