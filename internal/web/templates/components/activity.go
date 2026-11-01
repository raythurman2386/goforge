package components

import (
	"fmt"
	"net/url"
	"time"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components/ui"
)

// ActivityRow is one audit_logs row as the activity table shows it.
type ActivityRow struct {
	ID          string
	When        time.Time
	Source      string
	Action      string
	TokenName   string
	Resource    string
	ResourceURL templ.SafeURL
	ApprovalRef string
	Error       string
	Failed      bool
	// RelatedURL lists this change and its outcome rows (the origin row's
	// audit id, or this row's own id when it is the origin).
	RelatedURL templ.SafeURL
}

// ActivityRows turns audit rows into table rows, keeping their order.
func ActivityRows(logs []*models.AuditLog) []ActivityRow {
	out := make([]ActivityRow, 0, len(logs))
	for _, l := range logs {
		out = append(out, activityRow(l))
	}
	return out
}

func activityRow(l *models.AuditLog) ActivityRow {
	d, err := l.GetDetails()
	if err != nil {
		d = nil // unreadable details: show the row without them
	}
	row := ActivityRow{
		ID:          l.ID,
		When:        l.CreatedAt,
		Source:      l.Source(),
		Action:      l.Action,
		TokenName:   detailString(d, "token_name"),
		ApprovalRef: detailString(d, "approval_ref"),
		Error:       detailString(d, "error"),
		Failed:      l.Failed(),
	}
	row.Resource, row.ResourceURL = activityResource(l, d)
	origin := detailString(d, "audit_id")
	if origin == "" {
		origin = l.ID
	}
	row.RelatedURL = templ.SafeURL("/settings/activity?audit_id=" + url.QueryEscape(origin))
	return row
}

func detailString(d map[string]interface{}, key string) string {
	if d == nil {
		return ""
	}
	switch v := d[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// activityProjectID finds the project a row belongs to: details.project_id,
// or details.target.project_id on API rows.
func activityProjectID(d map[string]interface{}) string {
	if pid := detailString(d, "project_id"); pid != "" {
		return pid
	}
	if t, ok := d["target"].(map[string]interface{}); ok {
		return detailString(t, "project_id")
	}
	return ""
}

// activityResource is the label and link for a row's resource. Rows whose
// resource has no page (or whose project is unknown) get a label only.
func activityResource(l *models.AuditLog, d map[string]interface{}) (string, templ.SafeURL) {
	id := l.ResourceID.String
	if !l.ResourceID.Valid {
		id = detailString(d, "resource_ref")
	}
	if l.ResourceType == "" {
		return "", ""
	}
	label := l.ResourceType
	if id != "" {
		label += " " + shortID(id)
	}
	if name := detailString(d, "name"); name != "" {
		label = l.ResourceType + " " + name
	}
	esc := url.PathEscape(id)
	switch l.ResourceType {
	case models.ResourceTypeProject:
		if id != "" {
			return label, templ.SafeURL("/projects/" + esc)
		}
	case models.ResourceTypeServiceInstance:
		if id != "" {
			return label, templ.SafeURL("/services/" + esc)
		}
	case models.ResourceTypeDeployment:
		if pid := activityProjectID(d); pid != "" && id != "" {
			return label, templ.SafeURL("/projects/" + url.PathEscape(pid) + "/deployments/" + esc)
		}
	case models.ResourceTypeEnvironment:
		if pid := activityProjectID(d); pid != "" {
			return label, templ.SafeURL("/projects/" + url.PathEscape(pid))
		}
	case models.ResourceTypeContainer:
		if id != "" {
			return label, templ.SafeURL("/containers/" + esc)
		}
	case models.ResourceTypeAPIToken:
		return label, templ.SafeURL("/settings/profile")
	case models.ResourceTypeGitSource:
		return label, templ.SafeURL("/settings/git")
	}
	return label, ""
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// activitySourceLabel is the badge text for a source.
func activitySourceLabel(source string) string {
	switch source {
	case models.AuditSourceMCP:
		return "MCP"
	case models.AuditSourceAPI:
		return "API"
	default:
		return "UI"
	}
}

func activitySourceTone(source string) ui.BadgeTone {
	switch source {
	case models.AuditSourceMCP:
		return ui.BadgePrimary
	case models.AuditSourceAPI:
		return ui.BadgeInfo
	default:
		return ui.BadgeNeutral
	}
}

// activityRowClass highlights failed rows.
func activityRowClass(r ActivityRow) string {
	if r.Failed {
		return "bg-danger/10"
	}
	return "hover:bg-muted/60"
}

func activityTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("Jan 2 15:04:05")
}

// activityColumns is the column count of ActivityTable.
func activityColumns(compact bool) int {
	if compact {
		return 5
	}
	return 7
}

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
