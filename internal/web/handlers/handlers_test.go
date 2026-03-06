package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestIndex(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		Index(w, req)

		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusSeeOther {
			t.Errorf("Index() status = %d, want %d", res.StatusCode, http.StatusSeeOther)
		}

		location := res.Header.Get("Location")
		if location != "/login" {
			t.Errorf("Index() Location = %q, want %q", location, "/login")
		}
	})

	t.Run("authenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		user := &models.User{ID: "1"}
		ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		Index(w, req)

		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusSeeOther {
			t.Errorf("Index() status = %d, want %d", res.StatusCode, http.StatusSeeOther)
		}

		location := res.Header.Get("Location")
		if location != "/dashboard" {
			t.Errorf("Index() Location = %q, want %q", location, "/dashboard")
		}
	})
}
