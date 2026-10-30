package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/middleware"
)

type countingAuthenticator struct{ calls int }

func (a *countingAuthenticator) Authenticate(_ context.Context, _, _ string) (*models.User, error) {
	a.calls++
	return nil, errors.New("invalid credentials")
}

func postLogin(h *AuthHandler, remote, xff, email, password string) *httptest.ResponseRecorder {
	form := url.Values{"email": {email}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	return rec
}

func TestLogin_ThrottleFailsClosed(t *testing.T) {
	SetProxyTrust(httputil.NewProxyTrust(nil, false))
	t.Cleanup(func() { SetProxyTrust(httputil.ProxyTrust{}) })

	lt := middleware.NewLoginThrottle(3, 0, 15*time.Minute, 15*time.Minute)
	t.Cleanup(lt.Stop)
	authn := &countingAuthenticator{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewAuthHandler(nil, nil, nil, authn, nil, false, logger).WithLoginThrottle(lt)

	// Behind Traefik, with a rotating spoofed leftmost XFF entry: all three
	// failures land on the real client IP.
	for i, spoof := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		rec := postLogin(h, "172.18.0.5:1", spoof+", 198.51.100.7", "admin@example.test", "wrong")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if authn.calls != 3 {
		t.Fatalf("authenticator calls = %d, want 3", authn.calls)
	}

	rec := postLogin(h, "172.18.0.5:1", "4.4.4.4, 198.51.100.7", "admin@example.test", "right")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "900" {
		t.Errorf("Retry-After = %q, want 900", got)
	}
	if authn.calls != 3 {
		t.Errorf("authenticator called during lockout (%d calls); must fail closed", authn.calls)
	}
	if !strings.Contains(rec.Body.String(), "Too many failed sign-in attempts") {
		t.Error("lockout page missing message")
	}

	// A different client is unaffected.
	if rec := postLogin(h, "172.18.0.5:1", "198.51.100.8", "admin@example.test", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("other client status = %d, want 401", rec.Code)
	}
}

// With CLOUDFLARE_PROXY_MODE off (default), rotating a spoofed
// CF-Connecting-IP through Traefik does not dodge the per-IP lockout.
func TestLogin_ThrottleIgnoresSpoofedCFByDefault(t *testing.T) {
	SetProxyTrust(httputil.NewProxyTrust(nil, false))
	t.Cleanup(func() { SetProxyTrust(httputil.ProxyTrust{}) })

	lt := middleware.NewLoginThrottle(3, 0, 15*time.Minute, 15*time.Minute)
	t.Cleanup(lt.Stop)
	authn := &countingAuthenticator{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewAuthHandler(nil, nil, nil, authn, nil, false, logger).WithLoginThrottle(lt)

	post := func(cf string) *httptest.ResponseRecorder {
		form := url.Values{"email": {"admin@example.test"}, "password": {"wrong"}}
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "172.18.0.5:1"
		req.Header.Set("CF-Connecting-IP", cf)
		req.Header.Set("X-Forwarded-For", "198.51.100.7")
		rec := httptest.NewRecorder()
		h.Login(rec, req)
		return rec
	}
	for i, spoof := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		if rec := post(spoof); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}
	if rec := post("4.4.4.4"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status after 3 failures with rotating CF = %d, want 429", rec.Code)
	}
}
