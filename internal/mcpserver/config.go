// Package mcpserver holds the GoForge MCP tools. Every tool is one call to
// the GoForge JSON API (/api/v1) with a scoped bearer token; the package holds
// no Docker socket and no database connection.
//
// Two transports use it: cmd/goforge-mcp serves it over stdio (the fallback
// for clients that only spawn a process), and internal/mcphttp serves it at
// /mcp on goforge serve with an in-process HTTP client.
package mcpserver

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Environment variables read by LoadConfig.
const (
	EnvURL         = "GOFORGE_MCP_URL"
	EnvToken       = "GOFORGE_MCP_TOKEN"      // #nosec G101 -- an env var name, not a credential
	EnvTokenFile   = "GOFORGE_MCP_TOKEN_FILE" // #nosec G101 -- an env var name, not a credential
	EnvAllowWrites = "GOFORGE_MCP_ALLOW_WRITES"
	EnvProtected   = "GOFORGE_MCP_PROTECTED"

	defaultURL = "http://127.0.0.1:8080"
)

// Config configures the MCP server.
type Config struct {
	// BaseURL is the GoForge server, without /api/v1.
	BaseURL string
	// Token is the raw API token. It is never logged.
	Token string
	// AllowWrites registers the mutation tools. Off by default.
	AllowWrites bool
	// WriteRefusal, when set and AllowWrites is false, answers a call to any
	// mutation tool with this text as a tool error instead of "unknown
	// tool". The HTTP transport sets it to an insufficient_scope message for
	// read-only tokens. Stdio leaves it empty.
	WriteRefusal string
	// Protected lists service instance IDs the MCP refuses to stop,
	// reconfigure, or re-image unless the token has the protected scope.
	// The server enforces its own list; this one is checked first.
	Protected []string
}

// LoadConfig reads the configuration from the environment.
func LoadConfig(getenv func(string) string) (*Config, error) {
	cfg := &Config{BaseURL: strings.TrimRight(strings.TrimSpace(getenv(EnvURL)), "/")}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultURL
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%s must be an http(s) URL", EnvURL)
	}

	cfg.Token = strings.TrimSpace(getenv(EnvToken))
	if path := strings.TrimSpace(getenv(EnvTokenFile)); cfg.Token == "" && path != "" {
		raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied token file path
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", EnvTokenFile, err)
		}
		cfg.Token = strings.TrimSpace(string(raw))
	}
	if cfg.Token == "" {
		return nil, errors.New(EnvToken + " or " + EnvTokenFile + " is required")
	}

	switch strings.ToLower(strings.TrimSpace(getenv(EnvAllowWrites))) {
	case "1", "true", "yes", "on":
		cfg.AllowWrites = true
	}

	for _, id := range strings.Split(getenv(EnvProtected), ",") {
		if id = strings.TrimSpace(id); id != "" {
			cfg.Protected = append(cfg.Protected, id)
		}
	}
	return cfg, nil
}
