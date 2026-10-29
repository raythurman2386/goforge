package apiv1

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

const maxApprovalRefLen = 200

// gate is embedded in every mutation request body.
//
// dry_run defaults to true: a request that does not say "dry_run": false only
// reports what it would do. A real run needs approval_ref, the id of the
// human approval (for example a Tater message id), which is written to
// audit_logs before the change starts.
type gate struct {
	DryRun      *bool  `json:"dry_run,omitempty"`
	ApprovalRef string `json:"approval_ref,omitempty"`
}

func (g *gate) dryRun() bool { return g.DryRun == nil || *g.DryRun }

// mutation describes one gated change. Target and Changes must never hold a
// secret value: they are returned to the caller and stored in audit_logs.
type mutation struct {
	Action       string
	ResourceType string
	ResourceID   string
	Target       map[string]any
	Plan         string
	Changes      any
	// ProtectedID is set when the change is a stop, config edit, or image
	// update of a protected service. It needs the protected scope.
	ProtectedID string
}

// mutationResponse is the body of every mutation answer.
type mutationResponse struct {
	Action      string         `json:"action"`
	DryRun      bool           `json:"dry_run"`
	Target      map[string]any `json:"target"`
	Plan        string         `json:"plan"`
	Changes     any            `json:"changes,omitempty"`
	ApprovalRef string         `json:"approval_ref,omitempty"`
	AuditID     string         `json:"audit_id,omitempty"`
	Result      any            `json:"result,omitempty"`
}

func validApprovalRef(ref string) bool {
	if ref == "" || len(ref) > maxApprovalRefLen {
		return false
	}
	for _, r := range ref {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// refuseProtected answers 403 protected_resource and returns true when
// protectedID is set and the token lacks the protected scope. It runs before
// any Docker call.
func (h *Handler) refuseProtected(w http.ResponseWriter, r *http.Request, protectedID, action string) bool {
	if protectedID == "" {
		return false
	}
	_, token := caller(r)
	if token != nil && token.HasScope(models.ScopeProtected) {
		return false
	}
	writeError(w, http.StatusForbidden, codeProtected,
		"service "+protectedID+" is protected; "+action+" needs a token with the protected scope")
	return true
}

// begin runs the gate. It answers the request and returns false when the
// change must not run: protected refusal (403), dry run (200 with the plan),
// missing approval_ref (400), or a failed audit insert (500). On true the
// audit row is written and the caller performs the change, then calls finish.
func (h *Handler) begin(w http.ResponseWriter, r *http.Request, g *gate, m *mutation) (auditID string, ok bool) {
	user, token := caller(r)
	if h.refuseProtected(w, r, m.ProtectedID, m.Action) {
		return "", false
	}

	ref := strings.TrimSpace(g.ApprovalRef)
	if g.dryRun() {
		writeJSON(w, http.StatusOK, mutationResponse{
			Action: m.Action, DryRun: true, Target: m.Target, Plan: m.Plan, Changes: m.Changes, ApprovalRef: ref,
		})
		return "", false
	}

	if !validApprovalRef(ref) {
		writeError(w, http.StatusBadRequest, codeApprovalRequired,
			"dry_run=false needs a non-empty approval_ref (at most 200 printable characters)")
		return "", false
	}

	if h.deps.Audit == nil {
		unavailable(w, "audit log")
		return "", false
	}
	details := map[string]any{
		"via":          "api_v1",
		"approval_ref": ref,
		"token_id":     token.ID,
		"token_name":   token.Name,
		"target":       m.Target,
	}
	if m.Changes != nil {
		details["changes"] = m.Changes
	}
	entry := h.auditEntry(r, user.ID, "api_v1."+m.Action, m.ResourceType, m.ResourceID, details)
	if err := h.deps.Audit.Create(r.Context(), entry); err != nil {
		h.logger.Error("api v1: audit insert failed; mutation refused", "action", m.Action, "error", err)
		internalError(w, "could not write the audit record; nothing was changed")
		return "", false
	}
	return entry.ID, true
}

// finish answers a completed change. On failure it adds a best-effort audit
// row so the approval_ref trail shows the outcome.
func (h *Handler) finish(w http.ResponseWriter, r *http.Request, g *gate, m *mutation, auditID string, result any, err error, status int, msg string) {
	if err != nil {
		user, _ := caller(r)
		entry := h.auditEntry(r, user.ID, "api_v1."+m.Action+".failed", m.ResourceType, m.ResourceID, map[string]any{
			"via":          "api_v1",
			"approval_ref": strings.TrimSpace(g.ApprovalRef),
			"audit_id":     auditID,
			"error":        msg,
		})
		if auditErr := h.deps.Audit.Create(r.Context(), entry); auditErr != nil {
			h.logger.Warn("api v1: failure audit insert failed", "action", m.Action, "error", auditErr)
		}
		h.logger.Error("api v1: mutation failed", "action", m.Action, "resource_id", m.ResourceID, "error", err)
		writeError(w, status, codeForStatus(status), msg)
		return
	}
	writeJSON(w, http.StatusOK, mutationResponse{
		Action: m.Action, DryRun: false, Target: m.Target, Plan: m.Plan, Changes: m.Changes,
		ApprovalRef: strings.TrimSpace(g.ApprovalRef), AuditID: auditID, Result: result,
	})
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return codeBadRequest
	case http.StatusNotFound:
		return codeNotFound
	case http.StatusConflict:
		return codeConflict
	case http.StatusServiceUnavailable:
		return codeUnavailable
	default:
		return codeInternal
	}
}

// auditEntry builds an audit_logs row. resource_id is a UUID column, so a
// non-UUID id (a Docker container id) goes into details instead.
func (h *Handler) auditEntry(r *http.Request, userID, action, resourceType, resourceID string, details map[string]any) *models.AuditLog {
	entry := &models.AuditLog{
		ID:           uuid.New().String(),
		Action:       action,
		ResourceType: resourceType,
		CreatedAt:    time.Now(),
	}
	entry.UserID.String, entry.UserID.Valid = userID, userID != ""
	if _, err := uuid.Parse(resourceID); err == nil {
		entry.ResourceID.String, entry.ResourceID.Valid = resourceID, true
	} else if resourceID != "" {
		details["resource_ref"] = resourceID
	}
	if ip := h.clientIP(r); ip != "" && validIP(ip) {
		entry.IPAddress.String, entry.IPAddress.Valid = ip, true
	}
	if ua := r.UserAgent(); ua != "" {
		entry.UserAgent.String, entry.UserAgent.Valid = ua, true
	}
	raw, err := json.Marshal(details)
	if err != nil {
		raw = []byte("{}")
	}
	entry.Details = raw
	return entry
}
