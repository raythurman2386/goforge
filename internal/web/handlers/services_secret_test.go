package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

func newSecretHandler(t *testing.T) *ServiceHandler {
	t.Helper()
	h, repo := newEditHandler(t, models.ServiceStatusRunning)
	repo.inst.ConnectionInfo = []byte(`{"internal":"http://admin:keep-me@vw:80","external":"https://vw.example.com"}`)
	return h
}

func secretRequest(userID, source, key string) *http.Request {
	req := editRequest(http.MethodGet, userID, nil)
	req.URL.RawQuery = url.Values{"source": {source}, "key": {key}}.Encode()
	return req
}

func TestServiceHandler_Show_MasksSecrets(t *testing.T) {
	h := newSecretHandler(t)
	rr := httptest.NewRecorder()
	h.Show(rr, editRequest(http.MethodGet, "owner", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "keep-me") {
		t.Error("service page HTML contains the plain admin token")
	}
	for _, want := range []string{
		services.MaskedValue,
		"http://admin:" + services.MaskedValue + "@vw:80",
		`data-secret-url="/services/` + editTestInstanceID + `/secret?key=admin_token&amp;source=config"`,
		`data-secret-url="/services/` + editTestInstanceID + `/secret?key=internal&amp;source=connection"`,
		`data-secret-action="toggle"`,
		`data-secret-action="copy"`,
		"https://vw.example.com",
		"vw.example.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("service page missing %q", want)
		}
	}
	if strings.Count(body, "data-secret-url=") != 2 {
		t.Errorf("masked rows = %d, want 2 (admin_token, internal)", strings.Count(body, "data-secret-url="))
	}
}

func TestServiceHandler_RevealSecret(t *testing.T) {
	tests := []struct {
		name, user, source, key string
		wantCode                int
		wantBody                string
	}{
		{"owner reads config secret", "owner", "config", "admin_token", http.StatusOK, "keep-me"},
		{"owner reads masked connection", "owner", "connection", "internal", http.StatusOK, "http://admin:keep-me@vw:80"},
		{"non-secret config is not served", "owner", "config", "signups_allowed", http.StatusNotFound, ""},
		{"unmasked connection is not served", "owner", "connection", "external", http.StatusNotFound, ""},
		{"unknown source", "owner", "env", "admin_token", http.StatusNotFound, ""},
		{"other user forbidden", "intruder", "config", "admin_token", http.StatusForbidden, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newSecretHandler(t)
			rr := httptest.NewRecorder()
			h.RevealSecret(rr, secretRequest(tt.user, tt.source, tt.key))
			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantCode)
			}
			if tt.wantCode != http.StatusOK {
				if strings.Contains(rr.Body.String(), "keep-me") {
					t.Error("error response leaked the secret")
				}
				return
			}
			if rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
			if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Errorf("Content-Type = %q, want text/plain", ct)
			}
		})
	}
}
