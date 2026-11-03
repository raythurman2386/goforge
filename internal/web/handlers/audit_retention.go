package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/raythurman2386/goforge/internal/audit"
	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

// AuditRetentionSettings backs the admin audit retention card
// (audit.SettingsService).
type AuditRetentionSettings interface {
	View(ctx context.Context) (*audit.SettingsView, error)
	Save(ctx context.Context, raw, userID string) (oldDays, newDays int, changed bool, err error)
}

// WithAuditRetention shows the audit retention card to admins and enables
// PUT /settings/audit-retention.
func (h *SettingsHandler) WithAuditRetention(s AuditRetentionSettings) *SettingsHandler {
	h.auditRetention = s
	return h
}

func (h *SettingsHandler) auditRetentionForm(r *http.Request, user *models.User) *audit.SettingsView {
	if h.auditRetention == nil || user == nil || !user.IsAdmin() {
		return nil
	}
	v, err := h.auditRetention.View(r.Context())
	if err != nil {
		h.logger.Error("failed to read audit retention", "error", err)
		return nil
	}
	return v
}

// UpdateAuditRetention saves the retention card (admin only) and audits
// audit.settings.update with the old and new day counts.
func (h *SettingsHandler) UpdateAuditRetention(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user := requireAdmin(w, r)
	if user == nil {
		return
	}
	if h.auditRetention == nil {
		RenderError(w, r, http.StatusNotFound, "Audit retention is not available")
		return
	}
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "Invalid form")
		return
	}
	oldDays, newDays, changed, err := h.auditRetention.Save(r.Context(), r.FormValue("retention_days"), user.ID)
	if err != nil {
		var fe *audit.FieldError
		if errors.As(err, &fe) {
			RenderError(w, r, http.StatusBadRequest, fe.Field+" "+fe.Message)
			return
		}
		h.logger.Error("failed to save audit retention", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to save audit retention")
		return
	}
	if changed && h.audit != nil {
		h.audit.Record(r.Context(), user.ID, models.AuditActionAuditSettingsUpdate, models.ResourceTypeAuditSettings, "",
			map[string]interface{}{"old_days": oldDays, "new_days": newDays, "via": models.AuditViaUI},
			getClientIP(r), r.UserAgent())
	}
	h.logger.Info("audit retention saved", "user_id", user.ID, "old_days", oldDays, "new_days", newDays, "changed", changed)

	view := h.auditRetentionForm(r, user)
	if view == nil {
		view = &audit.SettingsView{Days: newDays, Source: audit.SourceSettings}
	}
	msg := "Audit retention saved"
	if !changed {
		msg = msgNoChanges
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := settings.AuditRetentionCard(view, auth.GetCSRFToken(r)).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render audit retention card", "error", err)
		return
	}
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}
