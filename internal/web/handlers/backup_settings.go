package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

// BackupSettings backs the admin Backups form (backup.SettingsService).
type BackupSettings interface {
	View(ctx context.Context) (*backup.SettingsView, error)
	Save(ctx context.Context, in backup.SettingsInput, userID string) ([]string, error)
	Test(ctx context.Context, in backup.SettingsInput) (string, error)
}

// AuditRecorder records an audit log entry (services.AuditService).
type AuditRecorder interface {
	Record(ctx context.Context, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string)
}

// WithBackupSettings makes the Backups card editable for admins and enables
// PUT /settings/backups and POST /settings/backups/test.
func (h *SettingsHandler) WithBackupSettings(s BackupSettings, audit AuditRecorder) *SettingsHandler {
	h.backupSettings = s
	h.audit = audit
	return h
}

// backupForm returns the form view, or nil (read-only card) when settings
// are not wired or cannot be read.
func (h *SettingsHandler) backupForm(r *http.Request) *backup.SettingsView {
	if h.backupSettings == nil {
		return nil
	}
	v, err := h.backupSettings.View(r.Context())
	if err != nil {
		h.logger.Error("failed to read backup settings", "error", err)
		return nil
	}
	return v
}

// backupFieldLabels names form fields in error messages.
var backupFieldLabels = map[string]string{
	"enabled": "Enabled", "endpoint": "Endpoint", "region": "Region", "bucket": "Bucket",
	"prefix": "Prefix", "access_key_id": "Access key ID", "secret_access_key": "Secret access key",
	"schedule": "Schedule", "retention_daily": "Keep daily backups", "timeout": "Run timeout",
}

// backupInput reads the Backups form. Checkboxes are on when present.
func backupInput(r *http.Request) backup.SettingsInput {
	on := func(name string) bool {
		v := r.PostFormValue(name)
		return v == "true" || v == "on" || v == "1"
	}
	return backup.SettingsInput{
		Enabled:          on("enabled"),
		Endpoint:         r.PostFormValue("endpoint"),
		Region:           r.PostFormValue("region"),
		Bucket:           r.PostFormValue("bucket"),
		Prefix:           r.PostFormValue("prefix"),
		AccessKeyID:      r.PostFormValue("access_key_id"),
		SecretAccessKey:  r.PostFormValue("secret_access_key"),
		ClearCredentials: on("clear_credentials"),
		ForcePathStyle:   on("force_path_style"),
		ScheduleOff:      on("schedule_off"),
		ScheduleTime:     r.PostFormValue("schedule_time"),
		ScheduleZone:     r.PostFormValue("schedule_zone"),
		RetentionDaily:   r.PostFormValue("retention_daily"),
		Timeout:          r.PostFormValue("timeout"),
	}
}

// backupFieldMessage turns a validation error into "Label: message". It
// reports false for any other error.
func backupFieldMessage(err error) (string, bool) {
	var fe *config.BackupFieldError
	if !errors.As(err, &fe) {
		return "", false
	}
	label := backupFieldLabels[fe.Field]
	if label == "" {
		label = fe.Field
	}
	return label + " " + fe.Message, true
}

// backupRequest checks the caller is an admin, settings are wired, and the
// form parses. It writes the error response and returns nil otherwise.
func (h *SettingsHandler) backupRequest(w http.ResponseWriter, r *http.Request) *models.User {
	w.Header().Set("Cache-Control", "no-store")
	user := requireAdmin(w, r)
	if user == nil {
		return nil
	}
	if h.backupSettings == nil {
		RenderError(w, r, http.StatusNotFound, "Backup settings are not available")
		return nil
	}
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "Invalid form")
		return nil
	}
	return user
}

// UpdateBackupSettings saves the Backups form (admin only) and applies it
// without a restart. It re-renders the card and audits the changed field
// names, never their values.
func (h *SettingsHandler) UpdateBackupSettings(w http.ResponseWriter, r *http.Request) {
	user := h.backupRequest(w, r)
	if user == nil {
		return
	}
	changed, err := h.backupSettings.Save(r.Context(), backupInput(r), user.ID)
	if err != nil {
		if msg, ok := backupFieldMessage(err); ok {
			RenderError(w, r, http.StatusBadRequest, msg)
			return
		}
		h.logger.Error("failed to save backup settings", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to save backup settings")
		return
	}
	if len(changed) > 0 && h.audit != nil {
		h.audit.Record(r.Context(), user.ID, models.AuditActionBackupSettingsUpdate, models.ResourceTypeBackupSettings, "",
			map[string]interface{}{"changed": changed}, getClientIP(r), r.UserAgent())
	}
	h.logger.Info("backup settings saved", "user_id", user.ID, "changed", changed)

	st := h.backupStatus(r, user)
	if st == nil {
		st = &backup.Status{}
	}
	msg := "Backup settings saved"
	if len(changed) == 0 {
		msg = "No changes to save"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := settings.BackupCard(st, h.backupForm(r), auth.GetCSRFToken(r)).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render backup card", "error", err)
		return
	}
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}

// TestBackupConnection lists at most one key under the form's prefix
// (admin only). Blank keys test the stored ones. Nothing is saved.
func (h *SettingsHandler) TestBackupConnection(w http.ResponseWriter, r *http.Request) {
	if h.backupRequest(w, r) == nil {
		return
	}
	msg, err := h.backupSettings.Test(r.Context(), backupInput(r))
	if err != nil {
		if fmsg, ok := backupFieldMessage(err); ok {
			RenderError(w, r, http.StatusBadRequest, fmsg)
			return
		}
		var pe *backup.ProbeError
		if errors.As(err, &pe) {
			RenderError(w, r, http.StatusBadGateway, "Connection failed: "+pe.Message)
			return
		}
		h.logger.Error("backup connection test failed", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Connection test failed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}

// RestorePointLister lists restore points (backup.RestorePointLister).
type RestorePointLister interface {
	List(ctx context.Context) *backup.RestorePointsView
}

// WithRestorePoints enables GET /settings/backups/restore-points, the
// read-only Restore points list on the Backups card.
func (h *SettingsHandler) WithRestorePoints(l RestorePointLister) *SettingsHandler {
	h.restorePoints = l
	return h
}

// RestorePoints renders the read-only Restore points list (admin only). The
// card loads it after the page so a slow bucket never delays the page.
// Restoring is CLI-only; the list shows the command to run.
func (h *SettingsHandler) RestorePoints(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if requireAdmin(w, r) == nil {
		return
	}
	if h.restorePoints == nil {
		RenderError(w, r, http.StatusNotFound, "Restore points are not available")
		return
	}
	v := h.restorePoints.List(r.Context())
	if v.Error != "" {
		h.logger.Warn("failed to list restore points", "error", v.Error)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := settings.BackupRestorePoints(v).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render restore points", "error", err)
	}
}
