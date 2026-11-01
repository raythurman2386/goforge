package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

const (
	activityPageSize   = 50
	activityRecentSize = 5
	activityMaxFilter  = 200
	activityMaxOffset  = 100000
)

// AuditLister reads audit rows for the activity views.
type AuditLister interface {
	ListFiltered(ctx context.Context, f repositories.AuditFilter) ([]*models.AuditLog, error)
}

// ActivityHandler serves /settings/activity and the dashboard's recent agent
// actions card.
type ActivityHandler struct {
	audit  AuditLister
	logger *slog.Logger
}

// NewActivityHandler creates an ActivityHandler.
func NewActivityHandler(audit AuditLister, logger *slog.Logger) *ActivityHandler {
	return &ActivityHandler{audit: audit, logger: logger}
}

func activitySource(s string) string {
	switch s {
	case models.AuditSourceMCP, models.AuditSourceAPI, models.AuditSourceUI, models.AuditSourceAgent:
		return s
	}
	return ""
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > activityMaxFilter {
		s = s[:activityMaxFilter]
	}
	return s
}

// Page renders the activity table. Users see their own rows; admins may add
// all=1 to see everyone's.
func (h *ActivityHandler) Page(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	q := r.URL.Query()
	f := settings.ActivityFilters{
		Source:  activitySource(q.Get("source")),
		Token:   clip(q.Get("token")),
		Action:  clip(q.Get("action")),
		AuditID: clip(q.Get("audit_id")),
		IsAdmin: user.IsAdmin(),
	}
	f.All = f.IsAdmin && q.Get("all") == "1"
	offset, err := strconv.Atoi(q.Get("offset"))
	if err != nil || offset < 0 || offset > activityMaxOffset {
		offset = 0
	}

	filter := repositories.AuditFilter{
		ActionContains: f.Action,
		Source:         f.Source,
		TokenName:      f.Token,
		AuditID:        f.AuditID,
		Limit:          activityPageSize + 1,
		Offset:         offset,
	}
	if !f.All {
		filter.UserID = user.ID
	}
	logs, err := h.audit.ListFiltered(r.Context(), filter)
	if err != nil {
		h.logger.Error("failed to list activity", "error", err)
		http.Error(w, "failed to load activity", http.StatusInternalServerError)
		return
	}
	if len(logs) > activityPageSize {
		logs = logs[:activityPageSize]
		f.OlderURL = activityURL(f, offset+activityPageSize)
	}
	if offset > 0 {
		f.NewerURL = activityURL(f, max(offset-activityPageSize, 0))
	}

	page := settings.Activity(components.ActivityRows(logs), f, auth.GetCSRFToken(r))
	if err := page.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render activity", "error", err)
	}
}

func activityURL(f settings.ActivityFilters, offset int) templ.SafeURL {
	v := url.Values{}
	for k, s := range map[string]string{"source": f.Source, "token": f.Token, "action": f.Action, "audit_id": f.AuditID} {
		if s != "" {
			v.Set(k, s)
		}
	}
	if f.All {
		v.Set("all", "1")
	}
	if offset > 0 {
		v.Set("offset", strconv.Itoa(offset))
	}
	if len(v) == 0 {
		return "/settings/activity"
	}
	return templ.SafeURL("/settings/activity?" + v.Encode())
}

// Recent renders the dashboard card: the user's latest MCP and API changes.
func (h *ActivityHandler) Recent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	logs, err := h.audit.ListFiltered(r.Context(), repositories.AuditFilter{
		UserID: user.ID,
		Source: models.AuditSourceAgent,
		Limit:  activityRecentSize,
	})
	if err != nil {
		h.logger.Error("failed to list recent agent actions", "error", err)
		http.Error(w, "failed to load recent agent actions", http.StatusInternalServerError)
		return
	}
	if err := components.ActivityCard(components.ActivityRows(logs)).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render recent agent actions", "error", err)
	}
}
