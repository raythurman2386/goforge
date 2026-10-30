package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/web/middleware"
)

// NO-SEC-HEADERS: every router response, including the login page, errors,
// and redirects, carries the hardening headers; HSTS only over HTTPS.
func TestRouter_SecurityHeaders(t *testing.T) {
	cfg := &config.Config{DevMode: true, Port: 8080, LogLevel: "info", CSRFSecret: "test-csrf-secret"}
	db := setupTestRouterDB(t)
	defer db.Close()
	router := setupTestRouter(t, db, cfg)

	tests := []struct {
		name     string
		path     string
		proto    string
		wantHSTS string
	}{
		{"login page over https", "/login", "https", middleware.HSTSValue},
		{"login page over http", "/login", "", ""},
		{"health", "/health", "https", middleware.HSTSValue},
		{"auth redirect", "/dashboard", "https", middleware.HSTSValue},
		{"not found", "/no-such-page", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.proto)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			want := map[string]string{
				"Content-Security-Policy":   "frame-ancestors 'none'; base-uri 'self'; object-src 'none'",
				"X-Frame-Options":           "DENY",
				"X-Content-Type-Options":    "nosniff",
				"Referrer-Policy":           "strict-origin-when-cross-origin",
				"Strict-Transport-Security": tt.wantHSTS,
			}
			for k, v := range want {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s %s: %s = %q, want %q (status %d)", tt.path, tt.proto, k, got, v, rec.Code)
				}
			}
		})
	}
}

// CSRF-SECRET-UNUSED: the router's CSRF group verifies tokens with
// cfg.CSRFSecret, so an unsigned matching cookie/header pair (the old
// double-submit bypass) is refused.
func TestRouter_CSRFRejectsUnsignedToken(t *testing.T) {
	cfg := &config.Config{DevMode: true, Port: 8080, LogLevel: "info", CSRFSecret: "test-csrf-secret"}
	db := setupTestRouterDB(t)
	defer db.Close()
	router := setupTestRouter(t, db, cfg)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.AddCookie(&http.Cookie{Name: "goforge_csrf", Value: "planted"})
	req.Header.Set("X-CSRF-Token", "planted")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /auth/login with unsigned token = %d, want 403", rec.Code)
	}
}
