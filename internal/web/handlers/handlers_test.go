package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raythurman2386/gocool/internal/auth"
	"github.com/raythurman2386/gocool/internal/models"
)

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	Health(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Health() status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	contentType := res.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Health() Content-Type = %q, want %q", contentType, "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("Health() failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("Health() status = %q, want %q", body["status"], "ok")
	}
}

func TestHealthReady(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()

	HealthReady(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("HealthReady() status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("HealthReady() failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("HealthReady() status = %v, want %q", body["status"], "ok")
	}

	checks, ok := body["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("HealthReady() missing checks field")
	}

	if checks["database"] != "ok" {
		t.Errorf("HealthReady() database check = %v, want %q", checks["database"], "ok")
	}
	if checks["docker"] != "ok" {
		t.Errorf("HealthReady() docker check = %v, want %q", checks["docker"], "ok")
	}
}

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

func TestLoginPage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()

	LoginPage(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("LoginPage() status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	contentType := res.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("LoginPage() Content-Type = %q, want text/html", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "GoCool") {
		t.Error("LoginPage() missing GoCool title")
	}
	if !strings.Contains(body, "/auth/github") {
		t.Error("LoginPage() missing GitHub auth link")
	}
	if !strings.Contains(body, "Sign in with GitHub") {
		t.Error("LoginPage() missing GitHub sign in button text")
	}
}

func TestHealth_HTTPMethods(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			w := httptest.NewRecorder()

			Health(w, req)

			res := w.Result()
			defer res.Body.Close()

			// Health handler should respond to any method (no method checking)
			if res.StatusCode != http.StatusOK {
				t.Errorf("Health() with %s = %d, want %d", method, res.StatusCode, http.StatusOK)
			}
		})
	}
}
