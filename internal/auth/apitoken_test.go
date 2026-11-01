package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

type stubTokens struct{ tokens map[string]*models.APIToken }

func (s stubTokens) GetByTokenHash(_ context.Context, hash string) (*models.APIToken, error) {
	if t, ok := s.tokens[hash]; ok {
		return t, nil
	}
	return nil, errors.New("not found")
}

func (stubTokens) UpdateLastUsed(context.Context, string) error { return nil }

type stubUsers struct{}

func (stubUsers) GetByID(_ context.Context, id string) (*models.User, error) {
	return &models.User{ID: id}, nil
}

func TestAPITokenMiddleware_Scopes(t *testing.T) {
	tokens := stubTokens{tokens: map[string]*models.APIToken{
		HashAPIToken("full"):   {ID: "t1", UserID: "u1", Scopes: []string{models.ScopeFull}},
		HashAPIToken("legacy"): {ID: "t2", UserID: "u1"},
		HashAPIToken("read"):   {ID: "t3", UserID: "u1", Scopes: []string{models.ScopeRead}},
		HashAPIToken("write"):  {ID: "t4", UserID: "u1", Scopes: []string{models.ScopeWrite, models.ScopeProtected}},
		// A full token restricted to a project is still /api/v1 only.
		HashAPIToken("fullrestricted"): {ID: "t5", UserID: "u1", Scopes: []string{models.ScopeFull}, ProjectIDs: []string{"33333333-3333-3333-3333-333333333333"}},
	}}
	cases := []struct {
		header   string
		want     int
		wantUser bool
	}{
		{"", http.StatusOK, false},
		{"Bearer full", http.StatusOK, true},
		{"Bearer legacy", http.StatusOK, true},
		{"Bearer read", http.StatusUnauthorized, false},
		{"Bearer write", http.StatusUnauthorized, false},
		{"Bearer fullrestricted", http.StatusUnauthorized, false},
		{"Bearer nope", http.StatusUnauthorized, false},
	}
	for _, c := range cases {
		var gotUser bool
		h := APITokenMiddleware(tokens, stubUsers{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser = r.Context().Value(UserContextKey) != nil
		}))
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want || gotUser != c.wantUser {
			t.Errorf("%q: status %d user %v, want %d %v", c.header, rec.Code, gotUser, c.want, c.wantUser)
		}
	}
}

func TestAuthenticateBearer(t *testing.T) {
	tokens := stubTokens{tokens: map[string]*models.APIToken{HashAPIToken("ok"): {ID: "t1", UserID: "u1", Scopes: []string{models.ScopeRead}}}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, _, err := AuthenticateBearer(req, tokens, stubUsers{}); !errors.Is(err, ErrNoBearerToken) {
		t.Errorf("no header: %v", err)
	}
	req.Header.Set("Authorization", "Bearer bad")
	if _, _, err := AuthenticateBearer(req, tokens, stubUsers{}); !errors.Is(err, ErrInvalidAPIToken) {
		t.Errorf("bad token: %v", err)
	}
	req.Header.Set("Authorization", "Bearer ok")
	u, tok, err := AuthenticateBearer(req, tokens, stubUsers{})
	if err != nil || u.ID != "u1" || tok.ID != "t1" {
		t.Errorf("ok token: %v %v %v", u, tok, err)
	}
}

func TestGenerateAPIToken(t *testing.T) {
	raw, hash, hint, err := GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, APITokenPrefix) || len(raw) != len(APITokenPrefix)+64 {
		t.Fatalf("raw token has the wrong shape (len %d)", len(raw))
	}
	if hash != HashAPIToken(raw) {
		t.Fatal("hash does not match HashAPIToken")
	}
	if hint != APITokenPrefix+"\u2026"+raw[len(raw)-4:] {
		t.Fatalf("hint = %q", hint)
	}
	raw2, _, _, _ := GenerateAPIToken()
	if raw2 == raw {
		t.Fatal("two tokens are equal")
	}
}

func TestAPITokenHint(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "",
		"short":               "",
		"0123456789abcdef":    "\u2026cdef",
		"gf_0123456789abcdef": "gf_\u2026cdef",
	} {
		if got := APITokenHint(in); got != want {
			t.Errorf("APITokenHint(%q) = %q, want %q", in, got, want)
		}
	}
}
