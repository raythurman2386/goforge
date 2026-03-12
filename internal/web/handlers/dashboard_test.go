package handlers

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDashboardHandler_Dashboard(t *testing.T) {
	t.Run("unauthenticated redirects to login", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		handler := NewDashboardHandler(nil, nil, nil, nil, logger)

		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		w := httptest.NewRecorder()

		handler.Dashboard(w, req)

		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusFound {
			t.Errorf("expected status %d, got %d", http.StatusFound, res.StatusCode)
		}
		if loc := res.Header.Get("Location"); loc != "/login" {
			t.Errorf("expected redirect to /login, got %s", loc)
		}
	})
}
