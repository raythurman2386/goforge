package apiv1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/errreport"
	"github.com/raythurman2386/goforge/internal/models"
)

const maxApprovalRefLen = 200

const (
	// defaultSyncWait is how long a real run waits for its change when the
	// request carries no deadline of its own.
	defaultSyncWait = 12 * time.Second
	// defaultOpTimeout bounds a change that runs on after its request. A
	// rollback stops and starts containers, so it gets minutes, not seconds.
	defaultOpTimeout = 10 * time.Minute
	// outcomeWriteTimeout bounds the .completed or .failed audit insert.
	outcomeWriteTimeout = 10 * time.Second
	// maxDeadlineMargin is the most of the request deadline kept back to
	// write the timeout answer before an outer timeout handler fires.
	maxDeadlineMargin = 2 * time.Second
)

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
	// Operation is set when the change goes on in the background after the
	// answer (a queued deployment, a service deploy or recreate).
	Operation *operation `json:"operation,omitempty"`
	// Replayed is true when this approval_ref already ran this change on
	// this target: nothing ran again and the first run's result is returned.
	Replayed bool `json:"replayed,omitempty"`
}

// pollTool is the MCP tool that waits on an operation.
const pollTool = "wait_for"

// operation points at background work a mutation started, so a client can
// wait for it with the wait_for MCP tool, or poll the matching read.
type operation struct {
	// Kind is "deployment" or "service".
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// ProjectID is set for a deployment; get_deployment needs it.
	ProjectID string `json:"project_id,omitempty"`
	PollTool  string `json:"poll_tool"`
}

func deploymentOp(projectID, deploymentID string) *operation {
	return &operation{Kind: "deployment", ID: deploymentID, ProjectID: projectID, PollTool: pollTool}
}

func serviceOp(instanceID string) *operation {
	return &operation{Kind: "service", ID: instanceID, PollTool: pollTool}
}

// outcome is what a gated change returns. Err nil means success.
type outcome struct {
	Result any
	Op     *operation
	Err    error
	Status int
	Msg    string
}

func succeeded(result any) outcome { return outcome{Result: result} }

// started is a success whose work goes on in the background.
func started(result any, op *operation) outcome { return outcome{Result: result, Op: op} }

func failed(err error, status int, msg string) outcome {
	return outcome{Err: err, Status: status, Msg: msg}
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
// audit row is written and the caller hands the change to run.
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
	key := idempotencyKey(m)
	details["idempotency_key"] = key
	if h.replay(w, r, user.ID, ref, m, key) {
		return "", false
	}
	entry := h.auditEntry(h.originOf(r), user.ID, "api_v1."+m.Action, m.ResourceType, m.ResourceID, details)
	if err := h.deps.Audit.Create(r.Context(), entry); err != nil {
		// A concurrent request with the same approval_ref won the insert
		// (unique index from migration 023); answer as that run.
		if errors.Is(err, repositories.ErrAlreadyExists) && h.replay(w, r, user.ID, ref, m, key) {
			return "", false
		}
		h.logger.Error("api v1: audit insert failed; mutation refused", "action", m.Action, "error", err)
		internalError(w, "could not write the audit record; nothing was changed")
		return "", false
	}
	return entry.ID, true
}

// idempotencyTargetFields are the target fields that tell two changes on
// the same resource apart: the variable key, the linked service, or the
// template and name of a new service. Fields that a change itself alters
// (a deployment status) are left out so a retry still matches.
var idempotencyTargetFields = []string{"key", "service_instance_id", "template", "name"}

// idempotencyKey names the target of an approved change. One approval_ref
// runs a given action on a given target at most once.
func idempotencyKey(m *mutation) string {
	parts := []string{m.ResourceType, m.ResourceID}
	for _, f := range idempotencyTargetFields {
		if v, ok := m.Target[f].(string); ok && v != "" {
			parts = append(parts, f+"="+v)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// replay answers a retry of an approved change and returns true when this
// approval_ref already started the same action on the same target:
//
//   - finished: 200 with the first run's result, operation, and audit_id,
//     and "replayed": true
//   - failed: the first run's error status and message with its audit_id
//   - still running: 409 in_progress with its audit_id
//
// Nothing runs again in any of these cases; a new attempt needs a new
// approval_ref. A failed lookup answers 500 and nothing runs.
func (h *Handler) replay(w http.ResponseWriter, r *http.Request, userID, ref string, m *mutation, key string) bool {
	action := "api_v1." + m.Action
	prior, err := h.deps.Audit.FindApproval(r.Context(), userID, action, ref, key)
	if err == nil && prior != nil {
		var done *models.AuditLog
		done, err = h.deps.Audit.FindOutcome(r.Context(), userID, action, prior.ID)
		if err == nil {
			h.answerReplay(w, ref, m, prior.ID, done)
			return true
		}
	}
	if err != nil {
		h.logger.Error("api v1: approval_ref lookup failed; mutation refused", "action", m.Action, "error", err)
		internalError(w, "could not check whether this approval_ref already ran; nothing was changed")
		return true
	}
	return false
}

func (h *Handler) answerReplay(w http.ResponseWriter, ref string, m *mutation, auditID string, done *models.AuditLog) {
	h.logger.Info("api v1: approval_ref already used for this change; returning the first run",
		"action", m.Action, "resource_id", m.ResourceID, "audit_id", auditID)
	if done == nil {
		writeJSON(w, http.StatusConflict, errorBody{Error: errorDetail{
			Code: codeInProgress,
			Message: fmt.Sprintf("approval_ref %q already started %s on this target and it has not finished; "+
				"its outcome is written to the audit log with this audit_id. Nothing was run again", ref, m.Action),
			AuditID:  auditID,
			Replayed: true,
		}})
		return
	}
	var d struct {
		Result    json.RawMessage `json:"result"`
		Operation *operation      `json:"operation"`
		Error     string          `json:"error"`
		Status    int             `json:"status"`
	}
	if err := json.Unmarshal(done.Details, &d); err != nil {
		h.logger.Warn("api v1: unreadable outcome audit row", "audit_id", auditID, "error", err)
	}
	if strings.HasSuffix(done.Action, ".failed") {
		status := d.Status
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
		msg := d.Error
		if msg == "" {
			msg = m.Action + " failed"
		}
		writeJSON(w, status, errorBody{Error: errorDetail{
			Code:     codeForStatus(status),
			Message:  msg + " (this approval_ref already ran this change and it failed; nothing was run again, a new attempt needs a new approval_ref)",
			AuditID:  auditID,
			Replayed: true,
		}})
		return
	}
	resp := mutationResponse{
		Action: m.Action, DryRun: false, Target: m.Target, Plan: m.Plan, Changes: m.Changes,
		ApprovalRef: ref, AuditID: auditID, Operation: d.Operation, Replayed: true,
	}
	if len(d.Result) > 0 {
		resp.Result = d.Result
	}
	writeJSON(w, http.StatusOK, resp)
}

// run performs an approved change and answers the request.
//
// The change runs on a context detached from the request, with its own
// deadline (OpTimeout), so a client that gives up or an outer request
// timeout never cancels it halfway: a rollback that stopped the current
// containers still starts the target. Its outcome is always written to
// audit_logs as api_v1.<action>.completed or .failed, on a fresh context.
//
// run waits for the change until shortly before the request's own deadline
// (or SyncWait when there is none). If the change is still going, run
// answers 504 with the JSON error code "timeout" and the audit_id to follow;
// the change keeps running and records its outcome when it ends.
func (h *Handler) run(w http.ResponseWriter, r *http.Request, g *gate, m *mutation, auditID string, op func(ctx context.Context) outcome) {
	user, token := caller(r)
	ref := strings.TrimSpace(g.ApprovalRef)
	origin := h.originOf(r)
	if token != nil {
		origin.tokenID, origin.tokenName = token.ID, token.Name
	}

	// The audit id rides on the op context so queued work (a deployment
	// in the queue, a service deploy or recreate) can write its final
	// deployment.run.* or service.run.* row linked to this audit row.
	opCtx, cancel := context.WithTimeout(models.WithAuditID(context.WithoutCancel(r.Context()), auditID), h.opTimeout)
	done := make(chan outcome, 1)
	go func() {
		defer cancel()
		out := h.safely(opCtx, m.Action, op)
		//nolint:contextcheck // The outcome row is written after the request may be gone; see above.
		h.recordOutcome(origin, user.ID, ref, m, auditID, out)
		done <- out
	}()

	wait := h.waitBudget(r.Context())
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case out := <-done:
		h.answer(w, g, m, auditID, out)
	case <-timer.C:
		h.logger.Warn("api v1: mutation still running after the request wait; answering timeout",
			"action", m.Action, "resource_id", m.ResourceID, "audit_id", auditID, "waited", wait.String())
		writeJSON(w, http.StatusGatewayTimeout, errorBody{Error: errorDetail{
			Code: codeTimeout,
			Message: fmt.Sprintf("%s did not finish within %s; it keeps running and its outcome is written to the "+
				"audit log as api_v1.%s.completed or api_v1.%s.failed with this audit_id", m.Action, wait.Round(time.Millisecond), m.Action, m.Action),
			AuditID: auditID,
		}})
	case <-r.Context().Done():
		// The caller left or an outer deadline already answered. The
		// change keeps running and still records its outcome.
		h.logger.Warn("api v1: request ended before the mutation finished; it keeps running",
			"action", m.Action, "resource_id", m.ResourceID, "audit_id", auditID)
	}
}

// safely runs op and turns a panic into a failed outcome, so a bug in one
// change cannot take the process down from a detached goroutine.
func (h *Handler) safely(ctx context.Context, action string, op func(ctx context.Context) outcome) (out outcome) {
	defer func() {
		if p := recover(); p != nil {
			h.logger.Error("api v1: mutation panicked", "action", action, "panic", p)
			errreport.CapturePanic(ctx, "api_v1."+action, p)
			out = failed(fmt.Errorf("panic: %v", p), http.StatusInternalServerError, "internal error while running "+action)
		}
	}()
	return op(ctx)
}

// waitBudget is how long run waits for a change before answering timeout:
// the time left before the request deadline, minus a margin to write the
// answer, or SyncWait when the request has no deadline.
func (h *Handler) waitBudget(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return h.syncWait
	}
	left := time.Until(dl)
	margin := min(left/5, maxDeadlineMargin)
	if budget := left - margin; budget > 0 {
		return budget
	}
	return time.Millisecond
}

// recordOutcome writes api_v1.<action>.completed or .failed. It uses its own
// context because the request may be long gone.
func (h *Handler) recordOutcome(origin origin, userID, ref string, m *mutation, auditID string, out outcome) {
	if h.deps.Audit == nil {
		return
	}
	details := map[string]any{"via": "api_v1", "approval_ref": ref, "audit_id": auditID}
	if origin.tokenName != "" {
		details["token_id"], details["token_name"] = origin.tokenID, origin.tokenName
	}
	action := "api_v1." + m.Action + ".completed"
	if out.Err != nil {
		action = "api_v1." + m.Action + ".failed"
		details["error"] = out.Msg
		details["status"] = out.Status
		h.logger.Error("api v1: mutation failed", "action", m.Action, "resource_id", m.ResourceID, "error", out.Err)
	} else {
		if out.Result != nil {
			details["result"] = out.Result
		}
		if out.Op != nil {
			details["operation"] = out.Op
		}
	}
	entry := h.auditEntry(origin, userID, action, m.ResourceType, m.ResourceID, details)
	ctx, cancel := context.WithTimeout(context.Background(), outcomeWriteTimeout)
	defer cancel()
	if err := h.deps.Audit.Create(ctx, entry); err != nil {
		h.logger.Warn("api v1: outcome audit insert failed", "action", action, "audit_id", auditID, "error", err)
	}
}

// answer writes the response for a finished change.
func (h *Handler) answer(w http.ResponseWriter, g *gate, m *mutation, auditID string, out outcome) {
	if out.Err != nil {
		writeError(w, out.Status, codeForStatus(out.Status), out.Msg)
		return
	}
	writeJSON(w, http.StatusOK, mutationResponse{
		Action: m.Action, DryRun: false, Target: m.Target, Plan: m.Plan, Changes: m.Changes,
		ApprovalRef: strings.TrimSpace(g.ApprovalRef), AuditID: auditID, Result: out.Result, Operation: out.Op,
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

// origin is the caller address and user agent recorded in audit_logs,
// captured while the request is live so later outcome rows can reuse it.
type origin struct {
	ip, userAgent string
	// tokenID and tokenName name the API token on outcome rows so the
	// activity page can filter them by token like the request row.
	tokenID, tokenName string
}

func (h *Handler) originOf(r *http.Request) origin {
	o := origin{userAgent: r.UserAgent()}
	if ip := h.clientIP(r); ip != "" && validIP(ip) {
		o.ip = ip
	}
	return o
}

// auditEntry builds an audit_logs row. resource_id is a UUID column, so a
// non-UUID id (a Docker container id) goes into details instead.
func (h *Handler) auditEntry(o origin, userID, action, resourceType, resourceID string, details map[string]any) *models.AuditLog {
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
	if o.ip != "" {
		entry.IPAddress.String, entry.IPAddress.Valid = o.ip, true
	}
	if o.userAgent != "" {
		entry.UserAgent.String, entry.UserAgent.Valid = o.userAgent, true
	}
	raw, err := json.Marshal(details)
	if err != nil {
		raw = []byte("{}")
	}
	entry.Details = raw
	return entry
}
