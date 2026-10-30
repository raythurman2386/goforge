package settings

import (
	"encoding/json"
	"strings"
)

// MCPTokenEnv is the environment variable the mcp.json snippet reads the
// token from, so the token never sits in the file.
const MCPTokenEnv = "GOFORGE_MCP_TOKEN" //nolint:gosec // G101: an env var name, not a credential

// MCP tool counts on /mcp: read tokens see the read tools; a write token
// also sees the gated mutation tools.
const (
	MCPReadTools  = 23
	MCPWriteTools = 37
)

// MCPCard is the Profile page's MCP card.
type MCPCard struct {
	// Endpoint is GOFORGE_BASE_URL + "/mcp".
	Endpoint string
	// Snippet is a copy-ready mcp.json (Cursor and other clients).
	Snippet string
}

// NewMCPCard builds the card for a base URL (GOFORGE_BASE_URL).
func NewMCPCard(baseURL string) *MCPCard {
	endpoint := strings.TrimRight(baseURL, "/") + "/mcp"
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"goforge": map[string]any{
				"url":     endpoint,
				"headers": map[string]string{"Authorization": "Bearer ${env:" + MCPTokenEnv + "}"},
			},
		},
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(cfg) //nolint:errcheck // static map of strings
	return &MCPCard{Endpoint: endpoint, Snippet: strings.TrimSpace(b.String())}
}
