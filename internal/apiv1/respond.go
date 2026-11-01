package apiv1

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Error codes returned in the JSON error body.
const (
	codeUnauthorized      = "unauthorized"
	codeInsufficientScope = "insufficient_scope"
	codeForbidden         = "forbidden"
	codeNotFound          = "not_found"
	codeBadRequest        = "bad_request"
	codeApprovalRequired  = "approval_ref_required"
	codeProtected         = "protected_resource"
	// codeOutOfScope: the token is restricted to listed projects and
	// services and the request names another one.
	codeOutOfScope  = "resource_out_of_scope"
	codeImmutable   = "immutable_field"
	codeConflict    = "conflict"
	codeUnavailable = "unavailable"
	codeInternal    = "internal_error"
	// codeTimeout: a real run did not finish while the request waited. The
	// change keeps running; follow audit_id for its outcome.
	codeTimeout = "timeout"
	// codeInProgress: the same approval_ref already started this change on
	// this target and it has not finished. Nothing ran again.
	codeInProgress = "in_progress"
)

const maxBodyBytes = 1 << 20

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// AuditID is set when the error belongs to an approved change that was
	// already recorded, for example a timeout while it keeps running.
	AuditID string `json:"audit_id,omitempty"`
	// Replayed is true when the error is the stored outcome of an earlier
	// run with the same approval_ref; nothing ran again.
	Replayed bool `json:"replayed,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) //nolint:errcheck // client went away; nothing to do
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func notFound(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, codeNotFound, what+" not found")
}

func forbidden(w http.ResponseWriter, message string) {
	writeError(w, http.StatusForbidden, codeForbidden, message)
}

func badRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, codeBadRequest, message)
}

func internalError(w http.ResponseWriter, message string) {
	writeError(w, http.StatusInternalServerError, codeInternal, message)
}

func unavailable(w http.ResponseWriter, what string) {
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, what+" is not available")
}

// decodeBody reads a JSON body into v. An empty body leaves v unchanged.
func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
