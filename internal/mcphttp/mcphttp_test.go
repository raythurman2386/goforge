package mcphttp

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

const (
	userID   = "11111111-1111-1111-1111-111111111111"
	rawRead  = "gf_test-read-token-000000000000000000000000"
	rawWrite = "gf_test-write-token-00000000000000000000000"
	rawFull  = "legacy-full-token-0000000000000000000000000"
	rawOld   = "gf_test-expired-token-000000000000000000000"
	rawProt  = "gf_test-protected-only-00000000000000000000"
)

type fakeTokens struct{ byHash map[string]*models.APIToken }

func (f *fakeTokens) GetByTokenHash(_ context.Context, hash string) (*models.APIToken, error) {
	if t, ok := f.byHash[hash]; ok {
		return t, nil
	}
	return nil, repositories.ErrNotFound
}
func (f *fakeTokens) UpdateLastUsed(context.Context, string) error { return nil }

type fakeUsers struct{}

func (fakeUsers) GetByID(_ context.Context, id string) (*models.User, error) {
	return &models.User{ID: id, Email: "owner@example.com", Role: models.RoleUser}, nil
}

func tokens() *fakeTokens {
	past := sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
	mk := func(id string, exp sql.NullTime, scopes ...string) *models.APIToken {
		return &models.APIToken{ID: id, UserID: userID, Name: id, Scopes: scopes, ExpiresAt: exp}
	}
	return &fakeTokens{byHash: map[string]*models.APIToken{
		auth.HashAPIToken(rawRead):  mk("read", sql.NullTime{}, models.ScopeRead),
		auth.HashAPIToken(rawWrite): mk("write", sql.NullTime{}, models.ScopeRead, models.ScopeWrite),
		auth.HashAPIToken(rawFull):  mk("full", sql.NullTime{}, models.ScopeFull),
		auth.HashAPIToken(rawOld):   mk("old", past, models.ScopeRead),
		auth.HashAPIToken(rawProt):  mk("prot", sql.NullTime{}, models.ScopeProtected),
	}}
}

// innerCall is what the fake /api/v1 handler saw.
type innerCall struct {
	method, path, auth, remote, xff, ua string
	body                                map[string]any
}

// fakeAPI stands in for the /api/v1 router (no prefix on paths).
type fakeAPI struct {
	mu    sync.Mutex
	calls []innerCall
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := innerCall{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
		remote: r.RemoteAddr, xff: r.Header.Get("X-Forwarded-For"), ua: r.UserAgent()}
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		_ = json.Unmarshal(b, &c.body)
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/whoami":
		_, _ = io.WriteString(w, `{"user":{"id":"`+userID+`"},"grants":{"read":true},"protected_services":["0fe81d3f-50e8-4d5d-9802-25e52752c1e3"]}`)
	default:
		_, _ = io.WriteString(w, `{"ok":true}`)
	}
}

func (f *fakeAPI) seen() []innerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]innerCall(nil), f.calls...)
}

func newServer(t *testing.T) (*httptest.Server, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{}
	srv := httptest.NewServer(New(Deps{API: api, Tokens: tokens(), Users: fakeUsers{}}))
	t.Cleanup(srv.Close)
	return srv, api
}

type bearer struct {
	token string
	extra http.Header
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	for k, v := range b.extra {
		r.Header[k] = v
	}
	return http.DefaultTransport.RoundTrip(r)
}

func connect(t *testing.T, url, token string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: bearer{token: token}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func listTools(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, tool := range res.Tools {
		out[tool.Name] = true
	}
	return out
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (bool, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return res.IsError, sb.String()
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}`

// reply is a read and closed /mcp response.
type reply struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func post(t *testing.T, url string, h map[string]string) reply {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}
}

func TestAuthRefusals(t *testing.T) {
	srv, api := newServer(t)
	cases := []struct {
		name      string
		headers   map[string]string
		status    int
		challenge string
	}{
		{"no header", nil, 401, `Bearer realm="goforge-mcp"`},
		{"cookie only", map[string]string{"Cookie": "session=abc; goforge_session=abc"}, 401, `Bearer realm="goforge-mcp"`},
		{"basic auth", map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}, 401, `Bearer realm="goforge-mcp"`},
		{"unknown token", map[string]string{"Authorization": "Bearer gf_nope"}, 401, `error="invalid_token"`},
		{"expired token", map[string]string{"Authorization": "Bearer " + rawOld}, 401, `error="invalid_token"`},
		{"full token", map[string]string{"Authorization": "Bearer " + rawFull}, 403, `error="insufficient_scope"`},
		{"protected only", map[string]string{"Authorization": "Bearer " + rawProt}, 403, `error="insufficient_scope"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := post(t, srv.URL, tc.headers)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, tc.challenge) {
				t.Fatalf("WWW-Authenticate = %q, want it to contain %q", got, tc.challenge)
			}
			if body := string(resp.Body); strings.Contains(body, "gf_test") || strings.Contains(body, "legacy-full") {
				t.Fatalf("refusal echoed the token: %s", body)
			}
		})
	}
	if n := len(api.seen()); n != 0 {
		t.Fatalf("refused requests reached the API %d times", n)
	}
}

func TestCrossOriginRefused(t *testing.T) {
	srv, api := newServer(t)
	resp := post(t, srv.URL, map[string]string{"Authorization": "Bearer " + rawWrite, "Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", resp.StatusCode)
	}
	resp = post(t, srv.URL, map[string]string{"Authorization": "Bearer " + rawWrite, "Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Origin status = %d, want 403", resp.StatusCode)
	}
	if n := len(api.seen()); n != 0 {
		t.Fatalf("cross-origin requests reached the API %d times", n)
	}
	// Same-origin and non-browser clients pass.
	if resp := post(t, srv.URL, map[string]string{"Authorization": "Bearer " + rawWrite}); resp.StatusCode != http.StatusOK {
		t.Fatalf("plain client status = %d, want 200", resp.StatusCode)
	}
}

func TestGetIsNotAllowed(t *testing.T) {
	srv, _ := newServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+rawRead)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405 (stateless, no standalone stream)", resp.StatusCode)
	}
}

func TestToolInventoryByScope(t *testing.T) {
	srv, _ := newServer(t)
	read := listTools(t, connect(t, srv.URL, rawRead))
	write := listTools(t, connect(t, srv.URL, rawWrite))
	if len(read) != 24 {
		t.Errorf("read token lists %d tools, want 24", len(read))
	}
	if len(write) != 38 {
		t.Errorf("write token lists %d tools, want 38", len(write))
	}
	if read["set_env_var"] || !write["set_env_var"] {
		t.Errorf("set_env_var: read=%v write=%v, want false/true", read["set_env_var"], write["set_env_var"])
	}
}

func TestWhoamiUsesCallerToken(t *testing.T) {
	srv, api := newServer(t)
	cs := connect(t, srv.URL, rawRead)
	if isErr, text := call(t, cs, "whoami", nil); isErr || !strings.Contains(text, userID) {
		t.Fatalf("whoami = %v %q", isErr, text)
	}
	calls := api.seen()
	if len(calls) == 0 {
		t.Fatal("whoami never reached the API")
	}
	c := calls[len(calls)-1]
	if c.method != http.MethodGet || c.path != "/whoami" {
		t.Fatalf("inner call = %s %s, want GET /whoami (prefix stripped)", c.method, c.path)
	}
	if c.auth != "Bearer "+rawRead {
		t.Fatal("inner call did not carry the caller's own bearer token")
	}
	if !strings.HasPrefix(c.ua, "goforge-mcp-http") {
		t.Fatalf("inner User-Agent = %q", c.ua)
	}
}

func TestReadTokenWriteRefused(t *testing.T) {
	srv, api := newServer(t)
	cs := connect(t, srv.URL, rawRead)
	isErr, text := call(t, cs, "set_env_var", map[string]any{
		"project_id": "33333333-3333-3333-3333-333333333333", "environment_id": "55555555-5555-5555-5555-555555555555",
		"key": "K", "value": "v", "dry_run": false, "approval_ref": "x",
	})
	if !isErr || !strings.Contains(text, "insufficient_scope") {
		t.Fatalf("set_env_var with read token = %v %q, want insufficient_scope tool error", isErr, text)
	}
	if n := len(api.seen()); n != 0 {
		t.Fatalf("refused write reached the API %d times", n)
	}
}

func TestWriteForwardsGateAndClientIP(t *testing.T) {
	srv, api := newServer(t)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		HTTPClient:           &http.Client{Transport: bearer{token: rawWrite, extra: http.Header{"X-Forwarded-For": {"203.0.113.9"}}}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	isErr, text := call(t, cs, "set_env_var", map[string]any{
		"project_id": "33333333-3333-3333-3333-333333333333", "environment_id": "55555555-5555-5555-5555-555555555555",
		"key": "K", "value": "v", "dry_run": false, "approval_ref": "approval-1",
	})
	if isErr {
		t.Fatalf("set_env_var = %q", text)
	}
	calls := api.seen()
	last := calls[len(calls)-1]
	if last.method != http.MethodPost || !strings.HasSuffix(last.path, "/variables") {
		t.Fatalf("inner call = %s %s", last.method, last.path)
	}
	if last.body["approval_ref"] != "approval-1" || last.body["dry_run"] != false {
		t.Fatalf("gate fields not forwarded: %v", last.body)
	}
	if !strings.HasPrefix(last.remote, "127.0.0.1:") || last.xff != "203.0.113.9" {
		t.Fatalf("inner peer = %q xff %q, want the /mcp request's peer and forwarded header", last.remote, last.xff)
	}
}

func TestNotConfigured(t *testing.T) {
	srv := httptest.NewServer(New(Deps{}))
	defer srv.Close()
	if resp := post(t, srv.URL, map[string]string{"Authorization": "Bearer " + rawRead}); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestInProcessRefusesOtherPaths(t *testing.T) {
	tr := &inProcess{api: &fakeAPI{}}
	req, _ := http.NewRequest(http.MethodGet, "http://goforge.internal/settings/tokens", nil)
	resp, err := tr.RoundTrip(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("in-process transport served a path outside /api/v1")
	}
}

// TestInProcessIgnoresOuterRoute mounts /mcp on a chi router the way
// web.Router does: the outer route context must not leak into /api/v1, or a
// GET tool call is routed as POST.
func TestInProcessIgnoresOuterRoute(t *testing.T) {
	inner := chi.NewRouter()
	inner.Get("/whoami", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"ok":true}`) })
	outer := chi.NewRouter()
	var status int
	outer.Post("/mcp", func(w http.ResponseWriter, r *http.Request) {
		tr := &inProcess{api: inner, peer: peerOf(r), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://goforge.internal/api/v1/whoami", nil)
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Errorf("round trip: %v", err)
			return
		}
		_ = resp.Body.Close()
		status = resp.StatusCode
	})
	outer.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if status != http.StatusOK {
		t.Fatalf("inner GET /whoami = %d, want 200", status)
	}
}
