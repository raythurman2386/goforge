package handlers

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

// confirmNameField is the form field a destructive UI action (delete
// service, delete project) must carry, set to the resource's exact name.
const confirmNameField = "confirm_name"

// maxConfirmBody caps how much of a DELETE body is read for confirm_name.
const maxConfirmBody = 4 << 10

// confirmedName returns the confirm_name the request carries. htmx sends a
// DELETE's form values in the query string; a form-encoded body is read too,
// since net/http does not parse DELETE bodies.
func confirmedName(r *http.Request) string {
	if v := r.URL.Query().Get(confirmNameField); v != "" {
		return v
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		return r.FormValue(confirmNameField)
	}
	if r.Body == nil || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxConfirmBody))
	if err != nil {
		return ""
	}
	vals, err := url.ParseQuery(string(raw))
	if err != nil {
		return ""
	}
	return vals.Get(confirmNameField)
}

// requireTypedName answers 400 and returns false unless the request's
// confirm_name equals name (surrounding spaces ignored). The text is shown
// in the delete dialog's error node.
func requireTypedName(w http.ResponseWriter, r *http.Request, kind, name string) bool {
	if strings.TrimSpace(confirmedName(r)) == name && name != "" {
		return true
	}
	http.Error(w, "Type the "+kind+" name exactly to confirm: "+name, http.StatusBadRequest)
	return false
}
