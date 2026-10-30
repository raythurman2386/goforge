package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func csrfHandler(c *CSRF) http.Handler {
	return c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(GetCSRFToken(r)))
	}))
}

type csrfReq struct {
	method  string
	session string
	cookie  string
	header  string
	form    string
}

func doCSRF(h http.Handler, in csrfReq) *httptest.ResponseRecorder {
	var body *strings.Reader
	if in.form != "" {
		body = strings.NewReader(url.Values{CSRFFieldName: {in.form}}.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(in.method, "/x", body)
	if in.form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if in.session != "" {
		// What auth.Middleware puts in context for a valid session cookie.
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: in.session})
		sess := &models.Session{TokenHash: hashToken(in.session)}
		req = req.WithContext(context.WithValue(req.Context(), SessionContextKey, sess))
	}
	if in.cookie != "" {
		req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: in.cookie})
	}
	if in.header != "" {
		req.Header.Set(CSRFHeaderName, in.header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func csrfCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == CSRFCookieName {
			return c.Value
		}
	}
	return ""
}

func TestCSRF_AnonymousSignedToken(t *testing.T) {
	h := csrfHandler(NewCSRF("secret-A"))

	get := doCSRF(h, csrfReq{method: http.MethodGet})
	tok := csrfCookie(get)
	if !strings.HasPrefix(tok, "a.") || get.Body.String() != tok {
		t.Fatalf("GET issued token %q (body %q)", tok, get.Body.String())
	}

	if rec := doCSRF(h, csrfReq{method: http.MethodPost, cookie: tok, header: tok}); rec.Code != http.StatusOK {
		t.Errorf("POST with header token = %d, want 200", rec.Code)
	}
	if rec := doCSRF(h, csrfReq{method: http.MethodPost, cookie: tok, form: tok}); rec.Code != http.StatusOK {
		t.Errorf("POST with form token = %d, want 200", rec.Code)
	}
	if rec := doCSRF(h, csrfReq{method: http.MethodPost, cookie: tok}); rec.Code != http.StatusForbidden {
		t.Errorf("POST without token = %d, want 403", rec.Code)
	}
	if rec := doCSRF(h, csrfReq{method: http.MethodGet, cookie: tok}); csrfCookie(rec) != "" {
		t.Error("valid cookie was re-issued on GET")
	}
}

// Before this change any matching cookie/header pair passed. A cookie planted
// from a sibling subdomain must not verify without CSRF_SECRET.
func TestCSRF_PlantedCookieRejected(t *testing.T) {
	h := csrfHandler(NewCSRF("secret-A"))
	for _, planted := range []string{"attacker", "a.nonce.badsig", "a.nonce"} {
		rec := doCSRF(h, csrfReq{method: http.MethodPost, cookie: planted, header: planted})
		if rec.Code != http.StatusForbidden {
			t.Errorf("planted %q status = %d, want 403", planted, rec.Code)
		}
	}

	// A token signed with a different secret does not verify either.
	other := csrfCookie(doCSRF(csrfHandler(NewCSRF("secret-B")), csrfReq{method: http.MethodGet}))
	if rec := doCSRF(h, csrfReq{method: http.MethodPost, cookie: other, header: other}); rec.Code != http.StatusForbidden {
		t.Errorf("foreign-secret token status = %d, want 403", rec.Code)
	}
}

func TestCSRF_SessionBoundToken(t *testing.T) {
	c := NewCSRF("secret-A")
	h := csrfHandler(c)

	get := doCSRF(h, csrfReq{method: http.MethodGet, session: "session-1"})
	tok := csrfCookie(get)
	if !strings.HasPrefix(tok, "s.") {
		t.Fatalf("session token %q lacks s. prefix", tok)
	}
	if again := doCSRF(h, csrfReq{method: http.MethodGet, session: "session-1"}).Body.String(); again != tok {
		t.Error("session token is not stable across requests")
	}

	if rec := doCSRF(h, csrfReq{method: http.MethodPost, session: "session-1", header: tok}); rec.Code != http.StatusOK {
		t.Errorf("POST with session token = %d, want 200", rec.Code)
	}
	// Another session's token, an anonymous token, and a planted cookie fail.
	other := csrfCookie(doCSRF(h, csrfReq{method: http.MethodGet, session: "session-2"}))
	anon := csrfCookie(doCSRF(h, csrfReq{method: http.MethodGet}))
	for name, bad := range map[string]string{"other session": other, "anonymous": anon} {
		if rec := doCSRF(h, csrfReq{method: http.MethodPost, session: "session-1", cookie: bad, header: bad}); rec.Code != http.StatusForbidden {
			t.Errorf("%s token status = %d, want 403", name, rec.Code)
		}
	}
	if rec := doCSRF(csrfHandler(NewCSRF("secret-B")), csrfReq{method: http.MethodPost, session: "session-1", header: tok}); rec.Code != http.StatusForbidden {
		t.Errorf("token under a rotated secret = %d, want 403", rec.Code)
	}
}

func TestNewCSRF_EmptySecretIsRandom(t *testing.T) {
	a := csrfCookie(doCSRF(csrfHandler(NewCSRF("")), csrfReq{method: http.MethodGet, session: "s"}))
	b := csrfCookie(doCSRF(csrfHandler(NewCSRF("")), csrfReq{method: http.MethodGet, session: "s"}))
	if a == b {
		t.Error("empty secret produced a predictable key")
	}
}

// A session cookie that auth.Middleware rejected (and cleared) does not bind
// the token, so the next anonymous POST (login) still verifies.
func TestCSRF_UnvalidatedSessionCookieUsesAnonToken(t *testing.T) {
	h := csrfHandler(NewCSRF("secret-A"))
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "expired"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	tok := csrfCookie(rec)
	if !strings.HasPrefix(tok, "a.") {
		t.Fatalf("token %q, want anonymous", tok)
	}
	if rec := doCSRF(h, csrfReq{method: http.MethodPost, cookie: tok, header: tok}); rec.Code != http.StatusOK {
		t.Errorf("login POST after stale session = %d, want 200", rec.Code)
	}
}
