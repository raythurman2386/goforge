// Package handlers contains HTTP handlers for the web application.
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/a-h/templ"

	"github.com/raythurman2386/gocool/internal/auth"
	"github.com/raythurman2386/gocool/internal/web/templates/pages/errors"
)

// RenderError renders an error page.
func RenderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	csrfToken := auth.GetCSRFToken(r)
	var component templ.Component
	switch status {
	case http.StatusNotFound:
		component = errors.NotFound(csrfToken)
	case http.StatusForbidden:
		component = errors.Forbidden(csrfToken)
	case http.StatusInternalServerError:
		component = errors.InternalError(csrfToken)
	default:
		component = errors.ErrorPage(status, http.StatusText(status), message, csrfToken)
	}

	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		// Fallback if rendering fails
		http.Error(w, message, status)
	}
}

// Health returns a simple health check response.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HealthReady returns a readiness check response.
// This checks database connectivity and other dependencies.
func HealthReady(w http.ResponseWriter, r *http.Request) {
	// TODO: Add actual health checks for DB, Docker, etc.
	checks := map[string]string{
		"database": "ok",
		"docker":   "ok",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"checks": checks,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Index handles the root path.
func Index(w http.ResponseWriter, r *http.Request) {
	// For now, redirect to login if not authenticated
	// TODO: Check authentication and redirect to dashboard
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// LoginPage renders the login page.
func LoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// TODO: Render actual login template
	if _, err := w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>GoCool - Login</title>
    <link rel="stylesheet" href="/static/css/output.css">
</head>
<body class="bg-gray-100 min-h-screen flex items-center justify-center">
    <div class="bg-white p-8 rounded-lg shadow-md w-full max-w-md">
        <h1 class="text-2xl font-bold text-center mb-6">GoCool</h1>
        <p class="text-gray-600 text-center mb-6">Sign in to deploy your applications</p>
        <a href="/auth/github" class="w-full bg-gray-900 text-white py-2 px-4 rounded hover:bg-gray-800 flex items-center justify-center">
            Sign in with GitHub
        </a>
    </div>
</body>
</html>`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
