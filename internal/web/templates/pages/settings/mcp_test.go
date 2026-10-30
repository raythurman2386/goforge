package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/models"
)

func renderMCP(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestNewMCPCard_Snippet(t *testing.T) {
	card := NewMCPCard("https://goforge.example.test/")
	if card.Endpoint != "https://goforge.example.test/mcp" {
		t.Fatalf("endpoint = %q", card.Endpoint)
	}
	var cfg struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(card.Snippet), &cfg); err != nil {
		t.Fatalf("snippet is not JSON: %v\n%s", err, card.Snippet)
	}
	gf := cfg.MCPServers["goforge"]
	if gf.URL != card.Endpoint || gf.Headers["Authorization"] != "Bearer ${env:GOFORGE_MCP_TOKEN}" {
		t.Fatalf("snippet = %+v", gf)
	}
}

func TestProfile_MCPCard(t *testing.T) {
	user := &models.User{Email: "u@x", Role: models.RoleUser}
	got := html.UnescapeString(renderMCP(t, Profile(user, "tok", nil, nil, NewMCPCard("https://gf.example.test"))))
	for _, want := range []string{
		`id="mcp-card"`, "https://gf.example.test/mcp", `"Authorization": "Bearer ${env:GOFORGE_MCP_TOKEN}"`,
		`hx-get="/settings/tokens/new?preset=mcp"`, "Generate MCP token", "23 read-only tools", "all 37", "Full tokens are refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("profile missing %q", want)
		}
	}
	if off := renderMCP(t, Profile(user, "tok", nil, nil, nil)); strings.Contains(off, "mcp-card") {
		t.Error("MCP card shown with /mcp off")
	}
}

func TestAddAPITokenModal_MCPPreset(t *testing.T) {
	got := renderMCP(t, AddAPITokenModal("csrf", true))
	for _, want := range []string{
		"Generate MCP Token", `name="preset" value="mcp"`, `value="90" selected`,
		`value="read" checked`, `value="write" checked`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("MCP preset modal missing %q", want)
		}
	}
	if strings.Contains(got, `value="0"`) || strings.Contains(got, `value="protected" checked`) {
		t.Error("MCP preset offers never-expiring or pre-checks protected")
	}
	plain := renderMCP(t, AddAPITokenModal("csrf", false))
	if strings.Contains(plain, `name="preset"`) || !strings.Contains(plain, `value="0"`) || strings.Contains(plain, `value="read" checked`) {
		t.Error("plain modal changed")
	}
}

func TestAPITokenRow_HintAndRotate(t *testing.T) {
	tok := &models.APIToken{ID: "tid", Name: "agent", TokenHint: "gf_\u2026abcd", Scopes: []string{"read"},
		CreatedAt: time.Now(), ExpiresAt: sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true}}
	got := renderMCP(t, APITokenRow(tok))
	for _, want := range []string{"gf_\u2026abcd", `hx-post="/settings/tokens/tid/rotate"`, `hx-delete="/settings/tokens/tid"`, "Revoke"} {
		if !strings.Contains(got, want) {
			t.Errorf("row missing %q", want)
		}
	}
	tok.TokenHint = ""
	if strings.Contains(renderMCP(t, APITokenRow(tok)), "data-token-hint") {
		t.Error("legacy token without hint shows an empty hint")
	}
}

func TestTokenCreatedModal(t *testing.T) {
	got := renderMCP(t, TokenCreatedModal("API Token Rotated", "gf_secret", true))
	if !strings.Contains(got, "API Token Rotated") || !strings.Contains(got, `id="created-token"`) || !strings.Contains(got, "GOFORGE_MCP_TOKEN") {
		t.Fatalf("modal = %s", got)
	}
	if strings.Contains(renderMCP(t, TokenCreatedModal("API Token Created", "x", false)), "GOFORGE_MCP_TOKEN") {
		t.Error("non-MCP token shows the MCP hint")
	}
}
