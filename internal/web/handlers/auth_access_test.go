package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestRegistrationOpen(t *testing.T) {
	countErr := errors.New("db down")
	tests := []struct {
		name    string
		count   int
		err     error
		enabled bool
		want    bool
	}{
		{name: "empty table allows the first account", count: 0, want: true},
		{name: "empty table allows the first account even when registration is closed", count: 0, enabled: false, want: true},
		{name: "later accounts follow the flag when it is closed", count: 2, enabled: false, want: false},
		{name: "later accounts follow the flag when it is open", count: 2, enabled: true, want: true},
		{name: "a failed count does not open registration", count: 0, err: countErr, enabled: false, want: false},
		{name: "a failed count keeps an explicit open flag", count: 0, err: countErr, enabled: true, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registrationOpen(tt.count, tt.err, tt.enabled)
			if got != tt.want {
				t.Fatalf("registrationOpen() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequireAdmin(t *testing.T) {
	tests := []struct {
		name  string
		user  *models.User
		want  int
		allow bool
	}{
		{name: "missing session", want: http.StatusUnauthorized},
		{name: "signed-in user", user: &models.User{ID: "u", Role: models.RoleUser}, want: http.StatusForbidden},
		{name: "admin", user: &models.User{ID: "a", Role: models.RoleAdmin}, want: http.StatusOK, allow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/dashboard/prune", nil)
			if tt.user != nil {
				req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, tt.user))
			}
			rr := httptest.NewRecorder()
			got := requireAdmin(rr, req)
			if tt.allow {
				if got == nil || rr.Code != http.StatusOK {
					t.Fatalf("requireAdmin() user=%v status=%d", got, rr.Code)
				}
				return
			}
			if got != nil {
				t.Fatal("requireAdmin() returned a user")
			}
			if rr.Code != tt.want {
				t.Fatalf("status = %d, want %d", rr.Code, tt.want)
			}
		})
	}
}
