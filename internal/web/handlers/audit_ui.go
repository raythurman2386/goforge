package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

// UIAuditRecorder records audit rows for changes made in the web UI
// (services.AuditService). Record writes in the background; RecordAs writes
// a row with a chosen id before it returns.
type UIAuditRecorder interface {
	AuditRecorder
	RecordAs(ctx context.Context, id, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string)
}

// uiAudit writes audit rows for UI mutations. The zero value records nothing,
// so handlers built without WithAudit (most tests) keep working.
type uiAudit struct {
	rec UIAuditRecorder
}

// uiDetails returns details with via=ui added, never modifying the input.
func uiDetails(details map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(details)+1)
	for k, v := range details {
		out[k] = v
	}
	out["via"] = models.AuditViaUI
	return out
}

// record writes one row after a UI change succeeded. It is a no-op without a
// recorder or a signed-in user.
func (a uiAudit) record(r *http.Request, action, resourceType, resourceID string, details map[string]interface{}) {
	if a.rec == nil {
		return
	}
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		return
	}
	a.rec.Record(r.Context(), user.ID, action, resourceType, resourceID, uiDetails(details), getClientIP(r), r.UserAgent())
}

// start records the row for a UI change that finishes in the background and
// returns its id ("" without a recorder or user). Put the id on the context
// passed to the queue or InstanceService with models.WithAuditID, so the
// final deployment.run.* or service.run.* row links back here.
func (a uiAudit) start(r *http.Request, action, resourceType, resourceID string, details map[string]interface{}) string {
	if a.rec == nil {
		return ""
	}
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		return ""
	}
	id := uuid.New().String()
	a.rec.RecordAs(r.Context(), id, user.ID, action, resourceType, resourceID, uiDetails(details), getClientIP(r), r.UserAgent())
	return id
}

// fail records <action>.failed linked to the row start wrote (id), for a
// change that was refused before its background work began. It does nothing
// when id is empty.
func (a uiAudit) fail(r *http.Request, id, action, resourceType, resourceID string, err error) {
	if a.rec == nil || id == "" || err == nil {
		return
	}
	msg := err.Error()
	if len(msg) > maxAuditErrorLen {
		msg = strings.ToValidUTF8(msg[:maxAuditErrorLen-3], "") + "..."
	}
	a.record(r, action+".failed", resourceType, resourceID, map[string]interface{}{"audit_id": id, "error": msg})
}

// maxAuditErrorLen caps the error kept on a .failed row.
const maxAuditErrorLen = 500
