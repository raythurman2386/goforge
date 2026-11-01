package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// structured decodes a result's structuredContent into a map.
func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatal("no structuredContent")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("structuredContent %s is not an object: %v", raw, err)
	}
	return out
}

func wantError(t *testing.T, res *mcp.CallToolResult, text string, status int, code string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("not a tool error: %s", text)
	}
	if text == "" {
		t.Error("tool error lost its text content")
	}
	sc := structured(t, res)
	if sc["code"] != code || sc["status"] != float64(status) || sc["message"] == "" {
		t.Errorf("structuredContent = %v, want status %d code %s and a message", sc, status, code)
	}
}

func TestProtectedRefusalIsStructured(t *testing.T) {
	cs := connect(t, &Config{AllowWrites: true, Protected: []string{"client-protected"}}, &fakeAPI{})

	// Refused by the MCP server before any API call.
	res, text := callTool(t, cs, "update_service_image", map[string]any{"service_id": "client-protected"})
	wantError(t, res, text, http.StatusForbidden, codeProtected)

	// Refused by the API: its code and message pass through.
	res, text = callTool(t, cs, "update_service_image", map[string]any{"service_id": "api-protected"})
	wantError(t, res, text, http.StatusForbidden, "protected_resource")
	if !strings.Contains(text, "403") {
		t.Errorf("text %q lost the API status", text)
	}
}

func TestAPIAndInputErrorsAreStructured(t *testing.T) {
	cs := connect(t, &Config{}, &fakeAPI{})
	res, text := callTool(t, cs, "get_project", map[string]any{"project_id": "missing"})
	wantError(t, res, text, http.StatusNotFound, "not_found")
	if sc := structured(t, res); sc["message"] != "project not found" {
		t.Errorf("message %v, want the API's message", sc["message"])
	}

	res, text = callTool(t, cs, "get_project", map[string]any{"project_id": ""})
	wantError(t, res, text, http.StatusBadRequest, codeBadRequest)
}

func TestWriteRefusalIsStructured(t *testing.T) {
	cs := connect(t, &Config{WriteRefusal: "insufficient_scope: token lacks the write scope"}, &fakeAPI{})
	res, text := callTool(t, cs, "restart_service", map[string]any{"service_id": "svc"})
	wantError(t, res, text, http.StatusForbidden, codeWritesDisabled)
}

func TestSuccessCarriesStructuredObject(t *testing.T) {
	cs := connect(t, &Config{}, &fakeAPI{})
	res, text := callTool(t, cs, "get_project", map[string]any{"project_id": "p1"})
	if res.IsError {
		t.Fatalf("error: %s", text)
	}
	sc := structured(t, res)
	if sc["ok"] != true || sc["path"] != "/api/v1/projects/p1" {
		t.Errorf("structuredContent = %v", sc)
	}
	var fromText map[string]any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil || fromText["path"] != sc["path"] {
		t.Errorf("text content %q does not match structuredContent", text)
	}
}
