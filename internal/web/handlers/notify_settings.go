package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

// NotifySettings backs the admin Notifications form (notify.SettingsService).
type NotifySettings interface {
	View(ctx context.Context) (*notify.SettingsView, error)
	Save(ctx context.Context, in notify.SettingsInput, userID string) ([]string, error)
	Test(ctx context.Context, in notify.SettingsInput) (string, error)
}

// WithNotifySettings shows the Notifications card to admins and enables
// PUT /settings/notifications and POST /settings/notifications/test.
func (h *SettingsHandler) WithNotifySettings(s NotifySettings) *SettingsHandler {
	h.notifySettings = s
	return h
}

// notifyForm returns the form view for admins, or nil (card hidden).
func (h *SettingsHandler) notifyForm(r *http.Request, user *models.User) *notify.SettingsView {
	if h.notifySettings == nil || user == nil || !user.IsAdmin() {
		return nil
	}
	v, err := h.notifySettings.View(r.Context())
	if err != nil {
		h.logger.Error("failed to read notification settings", "error", err)
		return nil
	}
	return v
}

// notifyFieldLabels names form fields in error messages.
var notifyFieldLabels = map[string]string{
	"enabled": "Enabled", "kind": "Type", "url": "URL", "token": "Access token",
}

// notifyInput reads the Notifications form.
func notifyInput(r *http.Request) notify.SettingsInput {
	on := func(name string) bool {
		v := r.PostFormValue(name)
		return v == "true" || v == "on" || v == "1"
	}
	return notify.SettingsInput{
		Enabled:    on("enabled"),
		Kind:       r.PostFormValue("kind"),
		URL:        r.PostFormValue("url"),
		Token:      r.PostFormValue("token"),
		ClearToken: on("clear_token"),
	}
}

func notifyFieldMessage(err error) (string, bool) {
	var fe *notify.FieldError
	if !errors.As(err, &fe) {
		return "", false
	}
	label := notifyFieldLabels[fe.Field]
	if label == "" {
		label = fe.Field
	}
	return label + " " + fe.Message, true
}

// notifyRequest checks the caller is an admin, settings are wired, and the
// form parses. It writes the error response and returns nil otherwise.
func (h *SettingsHandler) notifyRequest(w http.ResponseWriter, r *http.Request) *models.User {
	w.Header().Set("Cache-Control", "no-store")
	user := requireAdmin(w, r)
	if user == nil {
		return nil
	}
	if h.notifySettings == nil {
		RenderError(w, r, http.StatusNotFound, "Notification settings are not available")
		return nil
	}
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "Invalid form")
		return nil
	}
	return user
}

// UpdateNotifySettings saves the Notifications form (admin only). It
// re-renders the card and audits the changed field names, never values.
func (h *SettingsHandler) UpdateNotifySettings(w http.ResponseWriter, r *http.Request) {
	user := h.notifyRequest(w, r)
	if user == nil {
		return
	}
	changed, err := h.notifySettings.Save(r.Context(), notifyInput(r), user.ID)
	if err != nil {
		if msg, ok := notifyFieldMessage(err); ok {
			RenderError(w, r, http.StatusBadRequest, msg)
			return
		}
		h.logger.Error("failed to save notification settings", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to save notification settings")
		return
	}
	if len(changed) > 0 && h.audit != nil {
		h.audit.Record(r.Context(), user.ID, models.AuditActionNotifySettingsUpdate, models.ResourceTypeNotifySettings, "",
			map[string]interface{}{"changed": changed}, getClientIP(r), r.UserAgent())
	}
	h.logger.Info("notification settings saved", "user_id", user.ID, "changed", changed)

	v := h.notifyForm(r, user)
	if v == nil {
		v = &notify.SettingsView{Kind: models.NotifyKindNtfy}
	}
	msg := "Notification settings saved"
	if len(changed) == 0 {
		msg = "No changes to save"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := settings.NotifyCard(v, auth.GetCSRFToken(r)).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render notification card", "error", err)
		return
	}
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}

// TestNotifySettings sends one test notification to the form's target
// (blank URL and token use the stored ones). Nothing is saved.
func (h *SettingsHandler) TestNotifySettings(w http.ResponseWriter, r *http.Request) {
	if h.notifyRequest(w, r) == nil {
		return
	}
	msg, err := h.notifySettings.Test(r.Context(), notifyInput(r))
	if err != nil {
		if fmsg, ok := notifyFieldMessage(err); ok {
			RenderError(w, r, http.StatusBadRequest, fmsg)
			return
		}
		var se *notify.SendError
		if errors.As(err, &se) {
			RenderError(w, r, http.StatusBadGateway, "Test failed: "+se.Message)
			return
		}
		h.logger.Error("notification test failed", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Test failed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}
