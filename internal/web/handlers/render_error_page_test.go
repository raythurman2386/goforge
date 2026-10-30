package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

// The full-page error is auth-aware: signed-in users keep the app shell and go
// back to the dashboard; signed-out visitors are sent to sign in instead of a
// /dashboard link that would only redirect them to /login.
func TestRenderErrorPageAuthAware(t *testing.T) {
	statuses := []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError, http.StatusMethodNotAllowed}
	for _, status := range statuses {
		t.Run(http.StatusText(status)+"/signed-out", func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/nope", nil)
			w := httptest.NewRecorder()
			RenderError(w, r, status, "boom")
			body := w.Body.String()
			if w.Code != status {
				t.Fatalf("status = %d, want %d", w.Code, status)
			}
			if !strings.Contains(body, `href="/login"`) {
				t.Error("signed-out error page has no sign-in link")
			}
			if strings.Contains(body, `href="/dashboard"`) {
				t.Error("signed-out error page links to /dashboard")
			}
			if strings.Contains(body, `id="sidebar"`) {
				t.Error("signed-out error page renders the app sidebar")
			}
		})
		t.Run(http.StatusText(status)+"/signed-in", func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/nope", nil)
			user := &models.User{ID: "u1", Email: "a@example.com"}
			r = r.WithContext(context.WithValue(r.Context(), auth.UserContextKey, user))
			w := httptest.NewRecorder()
			RenderError(w, r, status, "boom")
			body := w.Body.String()
			if w.Code != status {
				t.Fatalf("status = %d, want %d", w.Code, status)
			}
			for _, want := range []string{`id="sidebar"`, "Log out", "Back to dashboard", `href="/dashboard"`} {
				if !strings.Contains(body, want) {
					t.Errorf("signed-in error page missing %q", want)
				}
			}
			if strings.Contains(body, `href="/login"`) {
				t.Error("signed-in error page links to /login")
			}
		})
	}
}
