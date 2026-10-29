package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderErrorHTMXEscapesMessage(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	RenderError(w, r, http.StatusBadRequest, `invalid value: got "<img src=x onerror=alert(1)>"`)
	body := w.Body.String()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(body, "<img") {
		t.Fatalf("HTMX error body contains raw markup: %s", body)
	}
	if !strings.Contains(body, "&lt;img") {
		t.Fatalf("expected escaped markup in body: %s", body)
	}
}
