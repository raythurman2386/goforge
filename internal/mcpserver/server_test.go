package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testToken = "test-token-value"

var readTools = []string{
	"health", "whoami", "dashboard_stats", "list_projects", "get_project", "list_environments",
	"list_env_vars", "list_deployments", "get_deployment", "get_deployment_logs",
	"list_service_templates", "get_service_template", "list_services", "get_service",
	"list_containers", "get_container", "get_container_logs", "get_container_stats",
	"list_audit_log", "list_git_sources", "list_repos", "list_branches", "get_backup_status",
}

var writeTools = []string{
	"trigger_deployment", "cancel_deployment", "rollback_deployment", "restart_service",
	"restart_container", "set_env_var", "delete_env_var", "sync_env_vars", "link_service",
	"unlink_service", "deploy_service", "update_service_image", "update_service_config",
	"stop_container",
}

type recorded struct {
	method, path, query, auth string
	body                      map[string]any
}

type fakeAPI struct {
	mu       sync.Mutex
	calls    []recorded
	grantsPr bool
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	rec := recorded{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, auth: r.Header.Get("Authorization")}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &rec.body)
	}
	f.mu.Lock()
	f.calls = append(f.calls, rec)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/v1/whoami":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"grants":             map[string]bool{"read": true, "write": true, "protected": f.grantsPr},
			"protected_services": []string{"server-protected"},
		})
	case r.URL.Path == "/api/v1/containers/dock-prot":
		_, _ = w.Write([]byte(`{"docker_id":"dock-prot","service_instance_id":"server-protected"}`))
	case r.URL.Path == "/api/v1/services/api-protected/image":
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"protected_resource","message":"service api-protected is protected; update_service_image needs a token with the protected scope"}}`))
	case r.URL.Path == "/api/v1/projects/missing":
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"project not found"}}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"path":"` + r.URL.Path + `"}`))
	}
}

func (f *fakeAPI) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.method+" "+c.path)
	}
	return out
}

func (f *fakeAPI) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func connect(t *testing.T, cfg *Config, api *fakeAPI) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(api)
	t.Cleanup(ts.Close)
	cfg.BaseURL = ts.URL
	cfg.Token = testToken
	srv := New(cfg, ts.Client())

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.MCP().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return res, text.String()
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

func TestToolInventory(t *testing.T) {
	ro := toolNames(t, connect(t, &Config{}, &fakeAPI{}))
	if len(ro) != len(readTools) {
		t.Errorf("read-only server has %d tools, want %d", len(ro), len(readTools))
	}
	for _, n := range readTools {
		if ro[n] == nil || !ro[n].Annotations.ReadOnlyHint {
			t.Errorf("read tool %s missing or not read-only", n)
		}
	}
	for _, n := range writeTools {
		if ro[n] != nil {
			t.Errorf("write tool %s registered without GOFORGE_MCP_ALLOW_WRITES", n)
		}
	}

	rw := toolNames(t, connect(t, &Config{AllowWrites: true}, &fakeAPI{}))
	if len(rw) != len(readTools)+len(writeTools) {
		t.Errorf("read-write server has %d tools, want %d", len(rw), len(readTools)+len(writeTools))
	}
	for _, n := range writeTools {
		tool := rw[n]
		if tool == nil {
			t.Fatalf("write tool %s missing", n)
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(schema), `"dry_run"`) || !strings.Contains(string(schema), `"approval_ref"`) {
			t.Errorf("%s schema lacks the gate fields: %s", n, schema)
		}
	}
	for _, banned := range []string{"reveal_secret", "get_secret", "delete_project", "delete_service", "prune", "migrate"} {
		if rw[banned] != nil {
			t.Errorf("excluded tool %s is registered", banned)
		}
	}
}

func TestReadToolCallsAPIWithBearer(t *testing.T) {
	api := &fakeAPI{}
	cs := connect(t, &Config{}, api)
	res, text := callTool(t, cs, "get_container_logs", map[string]any{"container_id": "abc", "tail": 50, "since": "10m"})
	if res.IsError {
		t.Fatalf("error: %s", text)
	}
	c := api.last()
	if c.method != http.MethodGet || c.path != "/api/v1/containers/abc/logs" || c.auth != "Bearer "+testToken {
		t.Errorf("call %+v", c)
	}
	if !strings.Contains(c.query, "tail=50") || !strings.Contains(c.query, "since=10m") {
		t.Errorf("query %q", c.query)
	}
	if strings.Contains(text, testToken) {
		t.Error("token in tool output")
	}
}

func TestBackupStatusToolIsAGet(t *testing.T) {
	api := &fakeAPI{}
	cs := connect(t, &Config{}, api)
	if res, text := callTool(t, cs, "get_backup_status", map[string]any{}); res.IsError {
		t.Fatalf("error: %s", text)
	}
	if c := api.last(); c.method != http.MethodGet || c.path != "/api/v1/backups" {
		t.Errorf("call %+v", c)
	}
}

func TestAPIErrorIsToolError(t *testing.T) {
	cs := connect(t, &Config{}, &fakeAPI{})
	res, text := callTool(t, cs, "get_project", map[string]any{"project_id": "missing"})
	if !res.IsError || !strings.Contains(text, "404") || !strings.Contains(text, "not_found") {
		t.Errorf("got IsError=%v %q", res.IsError, text)
	}
	res, _ = callTool(t, cs, "get_project", map[string]any{"project_id": ""})
	if !res.IsError {
		t.Error("empty project_id accepted")
	}
}

func TestMutationPassesGate(t *testing.T) {
	api := &fakeAPI{}
	cs := connect(t, &Config{AllowWrites: true}, api)
	_, _ = callTool(t, cs, "restart_service", map[string]any{"service_id": "svc"})
	c := api.last()
	if c.path != "/api/v1/services/svc/restart" || c.body["dry_run"] != nil {
		t.Errorf("default call %+v (dry_run must be left to the server default)", c)
	}
	_, _ = callTool(t, cs, "restart_service", map[string]any{"service_id": "svc", "dry_run": false, "approval_ref": "t1250u"})
	c = api.last()
	if c.body["dry_run"] != false || c.body["approval_ref"] != "t1250u" {
		t.Errorf("gate not passed: %+v", c.body)
	}
	_, _ = callTool(t, cs, "update_service_config", map[string]any{"service_id": "svc", "config": map[string]any{"size": "large"}})
	c = api.last()
	if c.method != http.MethodPatch || c.path != "/api/v1/services/svc/config" {
		t.Errorf("config call %+v", c)
	}
}

func TestClientSideProtectedRefusal(t *testing.T) {
	api := &fakeAPI{}
	cs := connect(t, &Config{AllowWrites: true, Protected: []string{"client-protected"}}, api)
	for _, id := range []string{"client-protected", "server-protected"} {
		res, text := callTool(t, cs, "update_service_image", map[string]any{"service_id": id})
		if !res.IsError || !strings.Contains(text, "protected") {
			t.Errorf("%s: IsError=%v %q", id, res.IsError, text)
		}
	}
	res, text := callTool(t, cs, "stop_container", map[string]any{"container_id": "dock-prot"})
	if !res.IsError || !strings.Contains(text, "protected") {
		t.Errorf("stop: IsError=%v %q", res.IsError, text)
	}
	for _, p := range api.paths() {
		if strings.HasSuffix(p, "/image") || strings.HasSuffix(p, "/stop") {
			t.Fatalf("refused mutation reached the API: %v", api.paths())
		}
	}
	// Restart on a protected service is allowed.
	res, _ = callTool(t, cs, "restart_service", map[string]any{"service_id": "server-protected"})
	if res.IsError {
		t.Error("restart of a protected service was refused")
	}
}

func TestProtectedScopeLetsClientThrough(t *testing.T) {
	api := &fakeAPI{grantsPr: true}
	cs := connect(t, &Config{AllowWrites: true, Protected: []string{"client-protected"}}, api)
	res, text := callTool(t, cs, "update_service_image", map[string]any{"service_id": "client-protected"})
	if res.IsError {
		t.Fatalf("refused with protected grant: %s", text)
	}
	if api.last().path != "/api/v1/services/client-protected/image" {
		t.Errorf("last call %+v", api.last())
	}
}

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := LoadConfig(env(nil)); err == nil {
		t.Error("missing token accepted")
	}
	cfg, err := LoadConfig(env(map[string]string{EnvToken: " tok ", EnvProtected: "a, b,,", EnvAllowWrites: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != defaultURL || cfg.Token != "tok" || !cfg.AllowWrites || len(cfg.Protected) != 2 {
		t.Errorf("cfg %+v", cfg)
	}
	if _, ftpErr := LoadConfig(env(map[string]string{EnvToken: "x", EnvURL: "ftp://x"})); ftpErr == nil {
		t.Error("ftp URL accepted")
	}
	path := filepath.Join(t.TempDir(), "token")
	if writeErr := os.WriteFile(path, []byte("from-file\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	cfg, err = LoadConfig(env(map[string]string{EnvTokenFile: path, EnvURL: "http://goforge:8080/"}))
	if err != nil || cfg.Token != "from-file" || cfg.BaseURL != "http://goforge:8080" || cfg.AllowWrites {
		t.Errorf("file cfg %+v %v", cfg, err)
	}
}

func TestWriteToolNames(t *testing.T) {
	rw := toolNames(t, connect(t, &Config{AllowWrites: true}, &fakeAPI{}))
	got := map[string]bool{}
	for _, n := range WriteToolNames() {
		got[n] = true
		if rw[n] == nil {
			t.Errorf("WriteToolNames lists %s but it is not registered", n)
		}
	}
	for n := range rw {
		if !slices.Contains(readTools, n) && !got[n] {
			t.Errorf("registered write tool %s is missing from WriteToolNames", n)
		}
	}
	if len(got) != len(writeTools) {
		t.Errorf("WriteToolNames has %d names, want %d", len(got), len(writeTools))
	}
}

func TestWriteRefusal(t *testing.T) {
	const msg = "insufficient_scope: token lacks the write scope"
	api := &fakeAPI{}
	cs := connect(t, &Config{WriteRefusal: msg}, api)
	if n := len(toolNames(t, cs)); n != len(readTools) {
		t.Fatalf("refusing server lists %d tools, want %d", n, len(readTools))
	}
	res, text := callTool(t, cs, "set_env_var", map[string]any{"project_id": "p", "environment_id": "e", "key": "K", "value": "v"})
	if !res.IsError || text != msg {
		t.Fatalf("set_env_var with refusal = %v %q, want tool error %q", res.IsError, text, msg)
	}
	if len(api.paths()) != 0 {
		t.Fatalf("refused write reached the API: %v", api.paths())
	}
	// Reads are untouched.
	if res, _ := callTool(t, cs, "health", nil); res.IsError {
		t.Fatal("health failed with a write refusal configured")
	}

	// Without WriteRefusal (stdio), an unregistered write stays unknown.
	plain := connect(t, &Config{}, &fakeAPI{})
	if _, err := plain.CallTool(context.Background(), &mcp.CallToolParams{Name: "set_env_var", Arguments: map[string]any{}}); err == nil {
		t.Fatal("stdio read-only server answered an unregistered write tool")
	}
}
