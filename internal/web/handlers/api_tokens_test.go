package handlers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

type fakeTokenStore struct {
	mu   sync.Mutex
	rows map[string]*models.APIToken
	n    int
}

func (f *fakeTokenStore) Create(_ context.Context, t *models.APIToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	t.ID = fmt.Sprintf("tok-%d", f.n)
	c := *t
	f.rows[t.ID] = &c
	return nil
}

func (f *fakeTokenStore) GetByID(_ context.Context, id string) (*models.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.rows[id]; ok {
		c := *t
		return &c, nil
	}
	return nil, repositories.ErrNotFound
}

func (f *fakeTokenStore) ListByUserID(_ context.Context, userID string) ([]*models.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*models.APIToken
	for _, t := range f.rows {
		if t.UserID == userID {
			c := *t
			out = append(out, &c)
		}
	}
	return out, nil
}

func (f *fakeTokenStore) Rotate(_ context.Context, id, userID, hash, hint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.rows[id]
	if !ok || t.UserID != userID {
		return repositories.ErrNotFound
	}
	t.TokenHash, t.TokenHint = hash, hint
	return nil
}

func (f *fakeTokenStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeTokenStore) only(t *testing.T) *models.APIToken {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 1 {
		t.Fatalf("store has %d rows, want 1", len(f.rows))
	}
	for _, r := range f.rows {
		return r
	}
	return nil
}

const (
	tokenOwner = "owner-1"
	tokenOther = "other-1"
)

var createdToken = regexp.MustCompile(`id="created-token"[^>]*>([^<]+)<`)

func tokenHandler() (*SettingsHandler, *fakeTokenStore, *fakeAudit) {
	store := &fakeTokenStore{rows: map[string]*models.APIToken{}}
	a := &fakeAudit{}
	h := (&SettingsHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).WithAPITokens(store).WithAudit(a)
	return h, store, a
}

// tokenRequest is an HTMX request from tokenOwner with the {id} URL param set.
func tokenRequest(method, path, id string, form url.Values) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("HX-Request", "true")
	ctx := context.WithValue(r.Context(), auth.UserContextKey, &models.User{ID: tokenOwner})
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rc))
}

func TestCreateAPIToken_MCPPreset(t *testing.T) {
	h, store, a := tokenHandler()
	w := httptest.NewRecorder()
	h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", url.Values{"name": {"cursor"}, "preset": {"mcp"}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
	m := createdToken.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatal("created modal has no token")
	}
	raw := m[1]
	tok := store.only(t)
	if !strings.HasPrefix(raw, auth.APITokenPrefix) || tok.TokenHash != auth.HashAPIToken(raw) {
		t.Fatal("token is not gf_-prefixed or the stored hash does not match")
	}
	if tok.TokenHint != auth.APITokenHint(raw) || strings.Contains(tok.TokenHint, raw[3:len(raw)-4]) {
		t.Fatalf("hint = %q", tok.TokenHint)
	}
	if !slices.Equal(tok.Scopes, []string{models.ScopeRead, models.ScopeWrite}) || !tok.IsMCPCapable() {
		t.Fatalf("scopes = %v, want read+write", tok.Scopes)
	}
	if days := time.Until(tok.ExpiresAt.Time).Hours() / 24; !tok.ExpiresAt.Valid || days < 89 || days > 91 {
		t.Fatalf("expiry = %v, want 90 days", tok.ExpiresAt)
	}
	if len(a.actions) != 1 || a.actions[0] != models.AuditActionAPITokenCreate {
		t.Fatalf("audit = %v", a.actions)
	}
	for k, v := range a.details[0] {
		if s := fmt.Sprint(v); strings.Contains(s, raw) || strings.Contains(s, tok.TokenHash) || strings.Contains(s, tok.TokenHint) {
			t.Fatalf("audit detail %s carries the token, hash, or hint", k)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("token response is cacheable")
	}
}

func TestCreateAPIToken_MCPPresetRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"full", url.Values{"scopes": {"full"}}},
		{"protected only", url.Values{"scopes": {"protected"}}},
		{"never expires", url.Values{"expires_in": {"0"}}},
		{"odd expiry", url.Values{"expires_in": {"7"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, store, _ := tokenHandler()
			tc.form.Set("name", "x")
			tc.form.Set("preset", "mcp")
			w := httptest.NewRecorder()
			h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", tc.form))
			if w.Code != http.StatusBadRequest || len(store.rows) != 0 {
				t.Fatalf("status = %d rows = %d, want 400 and nothing stored", w.Code, len(store.rows))
			}
		})
	}
}

func TestCreateAPIToken_PlainStillAllowsFullAndNever(t *testing.T) {
	h, store, _ := tokenHandler()
	w := httptest.NewRecorder()
	h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", url.Values{"name": {"ci"}, "expires_in": {"0"}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}
	tok := store.only(t)
	if !tok.IsFull() || tok.ExpiresAt.Valid || tok.IsMCPCapable() {
		t.Fatalf("plain token = %+v, want full and never-expiring", tok)
	}
}

func TestRotateAPIToken(t *testing.T) {
	h, store, a := tokenHandler()
	w := httptest.NewRecorder()
	h.CreateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens", "", url.Values{"name": {"cursor"}, "preset": {"mcp"}}))
	oldRaw := createdToken.FindStringSubmatch(w.Body.String())[1]
	before := store.only(t)

	w = httptest.NewRecorder()
	h.RotateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens/"+before.ID+"/rotate", before.ID, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "API Token Rotated") {
		t.Fatalf("rotate = %d %s", w.Code, w.Body)
	}
	newRaw := createdToken.FindStringSubmatch(w.Body.String())[1]
	after := store.only(t)
	if newRaw == oldRaw || after.TokenHash != auth.HashAPIToken(newRaw) || after.TokenHash == auth.HashAPIToken(oldRaw) {
		t.Fatal("rotate did not replace the secret")
	}
	if after.ID != before.ID || after.Name != before.Name || !slices.Equal(after.Scopes, before.Scopes) || !after.ExpiresAt.Time.Equal(before.ExpiresAt.Time) {
		t.Fatal("rotate changed the row's id, name, scopes, or expiry")
	}
	if after.TokenHint != auth.APITokenHint(newRaw) {
		t.Fatal("hint not updated")
	}
	if w.Header().Get("HX-Trigger") != "tokenCreated" {
		t.Error("list not refreshed")
	}
	if !slices.Equal(a.actions, []string{models.AuditActionAPITokenCreate, models.AuditActionAPITokenRotate}) {
		t.Fatalf("audit = %v", a.actions)
	}
}

func TestRotateAndRevoke_OtherUsersToken404(t *testing.T) {
	h, store, a := tokenHandler()
	store.rows["theirs"] = &models.APIToken{ID: "theirs", UserID: tokenOther, Name: "x", TokenHash: "h", Scopes: []string{"read"}}
	for _, call := range []func(http.ResponseWriter, *http.Request){h.RotateAPIToken, h.DeleteAPIToken} {
		w := httptest.NewRecorder()
		call(w, tokenRequest(http.MethodPost, "/settings/tokens/theirs", "theirs", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	}
	if store.rows["theirs"].TokenHash != "h" || len(a.actions) != 0 {
		t.Fatal("another user's token changed or was audited")
	}
}

func TestRotateExpiredToken400(t *testing.T) {
	h, store, _ := tokenHandler()
	store.rows["old"] = &models.APIToken{ID: "old", UserID: tokenOwner, TokenHash: "h", Scopes: []string{"read"}}
	store.rows["old"].ExpiresAt.Time, store.rows["old"].ExpiresAt.Valid = time.Now().Add(-time.Hour), true
	w := httptest.NewRecorder()
	h.RotateAPIToken(w, tokenRequest(http.MethodPost, "/settings/tokens/old/rotate", "old", nil))
	if w.Code != http.StatusBadRequest || store.rows["old"].TokenHash != "h" {
		t.Fatalf("status = %d, want 400 and no change", w.Code)
	}
}

func TestRevokeAPIToken_Audited(t *testing.T) {
	h, store, a := tokenHandler()
	store.rows["mine"] = &models.APIToken{ID: "mine", UserID: tokenOwner, Name: "agent", TokenHash: "h", Scopes: []string{"read"}}
	w := httptest.NewRecorder()
	h.DeleteAPIToken(w, tokenRequest(http.MethodDelete, "/settings/tokens/mine", "mine", nil))
	if w.Code != http.StatusOK || len(store.rows) != 0 {
		t.Fatalf("revoke = %d rows %d", w.Code, len(store.rows))
	}
	if !slices.Equal(a.actions, []string{models.AuditActionAPITokenRevoke}) || a.details[0]["name"] != "agent" {
		t.Fatalf("audit = %v %v", a.actions, a.details)
	}
}
