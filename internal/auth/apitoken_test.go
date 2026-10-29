package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
