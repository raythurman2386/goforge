package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/errreport"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

// ErrorReportingSettings backs the admin Error reporting (Sentry) form
// (errreport.SettingsService).
type ErrorReportingSettings interface {
	View(ctx context.Context) (*errreport.SettingsView, error)
	Save(ctx context.Context, in errreport.SettingsInput, userID string) ([]string, error)
	Test(ctx context.Context, in errreport.SettingsInput) (string, error)
}

// WithErrorReporting shows the Error reporting card to admins and enables
// PUT /settings/error-reporting and POST /settings/error-reporting/test.
func (h *SettingsHandler) WithErrorReporting(s ErrorReportingSettings) *SettingsHandler {
	h.errReport = s
	return h
}

// errReportForm returns the form view for admins, or nil (card hidden).
func (h *SettingsHandler) errReportForm(r *http.Request, user *models.User) *errreport.SettingsView {
	if h.errReport == nil || user == nil || !user.IsAdmin() {
		return nil
	}
	v, err := h.errReport.View(r.Context())
	if err != nil {
		h.logger.Error("failed to read error reporting settings", "error", err)
		return nil
	}
	return v
}

// msgNoChanges is the toast for a save that changed nothing.
const msgNoChanges = "No changes to save"

var errReportFieldLabels = map[string]string{"dsn": "DSN", "environment": "Environment"}

func errReportInput(r *http.Request) errreport.SettingsInput {
	return errreport.SettingsInput{
		DSN: r.PostFormValue("dsn"),
		// A checkbox posts only when checked.
		ClearDSN:    r.PostFormValue("clear_dsn") != "",
		Environment: r.PostFormValue("environment"),
	}
}

func errReportFieldMessage(err error) (string, bool) {
	var fe *errreport.FieldError
	if !errors.As(err, &fe) {
		return "", false
	}
	label := errReportFieldLabels[fe.Field]
	if label == "" {
		label = fe.Field
	}
	return label + " " + fe.Message, true
}

// errReportRequest checks the caller is an admin, settings are wired, and
// the form parses. It writes the error response and returns nil otherwise.
func (h *SettingsHandler) errReportRequest(w http.ResponseWriter, r *http.Request) *models.User {
	w.Header().Set("Cache-Control", "no-store")
	user := requireAdmin(w, r)
	if user == nil {
		return nil
	}
	if h.errReport == nil {
		RenderError(w, r, http.StatusNotFound, "Error reporting settings are not available")
		return nil
	}
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "Invalid form")
		return nil
	}
	return user
}

// UpdateErrorReportingSettings saves the Error reporting form (admin only)
// and hot-applies it. It re-renders the card and audits the changed field
// names, never the DSN.
func (h *SettingsHandler) UpdateErrorReportingSettings(w http.ResponseWriter, r *http.Request) {
	user := h.errReportRequest(w, r)
	if user == nil {
		return
	}
	changed, err := h.errReport.Save(r.Context(), errReportInput(r), user.ID)
	if err != nil {
		if msg, ok := errReportFieldMessage(err); ok {
			RenderError(w, r, http.StatusBadRequest, msg)
			return
		}
		h.logger.Error("failed to save error reporting settings", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to save error reporting settings")
		return
	}
	if len(changed) > 0 && h.audit != nil {
		h.audit.Record(r.Context(), user.ID, models.AuditActionErrorReportingSettingsUpdate, models.ResourceTypeErrorReportingSettings, "",
			map[string]interface{}{"changed": changed}, getClientIP(r), r.UserAgent())
	}
	h.logger.Info("error reporting settings saved", "user_id", user.ID, "changed", changed)

	v := h.errReportForm(r, user)
	if v == nil {
		v = &errreport.SettingsView{}
	}
	msg := "Error reporting settings saved"
	if len(changed) == 0 {
		msg = msgNoChanges
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := settings.ErrorReportingCard(v, auth.GetCSRFToken(r)).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render error reporting card", "error", err)
		return
	}
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}

// TestErrorReportingSettings sends one test event to the form's DSN (blank
// uses the one in effect). Nothing is saved.
func (h *SettingsHandler) TestErrorReportingSettings(w http.ResponseWriter, r *http.Request) {
	if h.errReportRequest(w, r) == nil {
		return
	}
	msg, err := h.errReport.Test(r.Context(), errReportInput(r))
	if err != nil {
		if fmsg, ok := errReportFieldMessage(err); ok {
			RenderError(w, r, http.StatusBadRequest, fmsg)
			return
		}
		var se *errreport.SendError
		if errors.As(err, &se) {
			RenderError(w, r, http.StatusBadGateway, "Test failed: "+se.Message)
			return
		}
		h.logger.Error("error reporting test failed", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Test failed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := components.Toast(msg, "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}
