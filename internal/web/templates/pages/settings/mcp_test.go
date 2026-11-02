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
	got := html.UnescapeString(renderMCP(t, Profile(user, "tok", nil, nil, nil, nil, NewMCPCard("https://gf.example.test"))))
	for _, want := range []string{
		`id="mcp-card"`, "https://gf.example.test/mcp", `"Authorization": "Bearer ${env:GOFORGE_MCP_TOKEN}"`,
		`hx-get="/settings/tokens/new?preset=mcp"`, "Generate MCP token", "23 read-only tools", "all 37", "Full tokens are refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("profile missing %q", want)
		}
	}
	if off := renderMCP(t, Profile(user, "tok", nil, nil, nil, nil, nil)); strings.Contains(off, "mcp-card") {
		t.Error("MCP card shown with /mcp off")
	}
}

func TestAddAPITokenModal_MCPPreset(t *testing.T) {
	got := renderMCP(t, AddAPITokenModal("csrf", true, nil))
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
	plain := renderMCP(t, AddAPITokenModal("csrf", false, nil))
	if strings.Contains(plain, `name="preset"`) || !strings.Contains(plain, `value="0"`) || strings.Contains(plain, `value="read" checked`) {
		t.Error("plain modal changed")
	}
}

func TestAPITokenRow_HintAndRotate(t *testing.T) {
	tok := &models.APIToken{ID: "tid", Name: "agent", TokenHint: "gf_\u2026abcd", Scopes: []string{"read"},
		CreatedAt: time.Now(), ExpiresAt: sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true}}
	got := renderMCP(t, APITokenRow(tok, nil))
	for _, want := range []string{"gf_\u2026abcd", `hx-post="/settings/tokens/tid/rotate"`, `hx-delete="/settings/tokens/tid"`, "Revoke"} {
		if !strings.Contains(got, want) {
			t.Errorf("row missing %q", want)
		}
	}
	tok.TokenHint = ""
	if strings.Contains(renderMCP(t, APITokenRow(tok, nil)), "data-token-hint") {
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

// PR4: the token dialogs are ui.Dialogs. The form swaps its own dialog
// (no fixed #modal id), and the shown-once token is not dismissed by a
// stray backdrop click.
func TestTokenDialogs_KitContract(t *testing.T) {
	add := renderMCP(t, AddAPITokenModal("csrf", false, nil))
	for _, want := range []string{` id="dialog-`, `data-ui-dialog="ephemeral"`, `hx-target="closest dialog"`, `hx-swap="outerHTML"`, `data-ui-dialog-close`} {
		if !strings.Contains(add, want) {
			t.Errorf("AddAPITokenModal missing %q", want)
		}
	}
	if strings.Contains(add, `id="modal"`) || strings.Contains(add, "data-ui-dialog-nondismissible") {
		t.Error("AddAPITokenModal: legacy id or non-dismissible")
	}
	created := renderMCP(t, TokenCreatedModal("API Token Created", "gf_x", false))
	if !strings.Contains(created, "data-ui-dialog-nondismissible") || !strings.Contains(created, ">API Token Created</h2>") {
		t.Errorf("TokenCreatedModal = %s", created)
	}
	tok := &models.APIToken{ID: "tid", Name: "agent", CreatedAt: time.Now()}
	row := renderMCP(t, APITokenRow(tok, nil))
	for _, want := range []string{`aria-haspopup="dialog"`, `data-ui-confirm-label="Rotate"`, `data-ui-confirm-tone="danger"`, `data-ui-confirm-label="Revoke"`} {
		if !strings.Contains(row, want) {
			t.Errorf("token row missing %q", want)
		}
	}
}

// Copy buttons use the ui.js data-ui-copy contract (no templ script, no
// inline onclick), and the copied text stays in the page element.
func TestCopyButtonsUseKit(t *testing.T) {
	user := &models.User{Email: "u@x", Role: models.RoleUser}
	for name, got := range map[string]string{
		"profile": renderMCP(t, Profile(user, "tok", nil, nil, nil, nil, NewMCPCard("https://gf.example.test"))),
		"created": renderMCP(t, TokenCreatedModal("API Token Created", "gf_secret", false)),
	} {
		if strings.Contains(got, "onclick") || strings.Contains(got, "copyText") {
			t.Errorf("%s: inline copy handler left", name)
		}
	}
	prof := renderMCP(t, Profile(user, "tok", nil, nil, nil, nil, NewMCPCard("https://gf.example.test")))
	if !strings.Contains(prof, `data-ui-copy="mcp-snippet"`) || !strings.Contains(prof, `id="mcp-snippet"`) {
		t.Error("mcp.json copy button not wired to #mcp-snippet")
	}
	created := renderMCP(t, TokenCreatedModal("API Token Created", "gf_secret", false))
	if !strings.Contains(created, `data-ui-copy="created-token"`) || strings.Count(created, "gf_secret") != 1 {
		t.Errorf("token copy button not wired, or the token is repeated in markup: %s", created)
	}
}
