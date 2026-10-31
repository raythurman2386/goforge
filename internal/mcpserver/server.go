package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is reported to MCP clients and by goforge-mcp --version. Release
// builds set it from the git tag with -ldflags
// "-X github.com/raythurman2386/goforge/internal/mcpserver.Version=v0.1.0"
// (see the Makefile VERSION); a build without a tag reports "dev".
var Version = "dev"

// Server is the GoForge MCP server.
type Server struct {
	cfg *Config
	api *apiClient
	mcp *mcp.Server

	whoamiOnce sync.Once
	grantsProt bool
	protected  map[string]bool
}

// New builds the server and registers its tools. hc may be nil.
func New(cfg *Config, hc *http.Client) *Server {
	s := &Server{
		cfg: cfg,
		api: newAPIClient(cfg, hc),
		mcp: mcp.NewServer(&mcp.Implementation{Name: "goforge", Title: "GoForge", Version: Version}, &mcp.ServerOptions{
			Instructions: "Tools for the GoForge PaaS. Reads are safe. Every mutation defaults to dry_run=true and " +
				"returns a plan; to run it, call again with dry_run=false and approval_ref set to the id of the " +
				"human approval. Secrets are always masked; there is no tool that reveals one.",
		}),
	}
	s.registerReads()
	switch {
	case cfg.AllowWrites:
		s.registerWrites()
	case cfg.WriteRefusal != "":
		s.mcp.AddReceivingMiddleware(refuseWrites(cfg.WriteRefusal))
	}
	return s
}

// refuseWrites answers tools/call for an unregistered mutation tool with a
// tool error carrying msg, so a read-only client learns why instead of
// seeing "unknown tool".
func refuseWrites(msg string) mcp.Middleware {
	writes := make(map[string]bool, len(writeToolNames))
	for _, n := range writeToolNames {
		writes[n] = true
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				if ctr, ok := req.(*mcp.CallToolRequest); ok && ctr.Params != nil && writes[ctr.Params.Name] {
					return errorResult(msg), nil
				}
			}
			return next(ctx, method, req)
		}
	}
}

// WriteToolNames lists the mutation tools in registration order.
func WriteToolNames() []string { return append([]string(nil), writeToolNames...) }

// MCP returns the underlying MCP server.
func (s *Server) MCP() *mcp.Server { return s.mcp }

// Run serves MCP over stdin and stdout until the client disconnects. The
// HTTP transport does not call Run; it hands MCP() to the SDK's streamable
// HTTP handler.
func (s *Server) Run(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// call is one API request built from tool input.
type call struct {
	method string
	path   string
	query  map[string]string
	body   any
}

func boolPtr(b bool) *bool { return &b }

var (
	readOnly    = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPtr(false)}
	nonDestruct = &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)}
	destructive = &mcp.ToolAnnotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)}
)

// addTool registers a tool whose handler turns its input into one API call
// and returns the API's JSON as text. API errors become tool errors.
func addTool[In any](s *Server, name, desc string, ann *mcp.ToolAnnotations, build func(ctx context.Context, in In) (*call, error)) {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: name, Description: desc, Annotations: ann},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			c, err := build(ctx, in)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			q := make(map[string][]string, len(c.query))
			for k, v := range c.query {
				if v != "" {
					q[k] = []string{v}
				}
			}
			raw, err := s.api.do(ctx, c.method, c.path, q, c.body)
			if err != nil {
				var ae *apiError
				if errors.As(err, &ae) {
					return errorResult(ae.Error()), nil, nil
				}
				return errorResult(err.Error()), nil, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
		})
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// required takes name, value pairs and reports the first empty value.
func required(nameValue ...string) error {
	for i := 0; i+1 < len(nameValue); i += 2 {
		if nameValue[i+1] == "" {
			return fmt.Errorf("%s is required", nameValue[i])
		}
	}
	return nil
}

// loadWhoami caches the token's protected grant and the server's protected
// list. On failure only the client-side list applies.
func (s *Server) loadWhoami(ctx context.Context) {
	s.whoamiOnce.Do(func() {
		s.protected = map[string]bool{}
		for _, id := range s.cfg.Protected {
			s.protected[id] = true
		}
		raw, err := s.api.do(ctx, http.MethodGet, "/whoami", nil, nil)
		if err != nil {
			return
		}
		var who struct {
			Grants            map[string]bool `json:"grants"`
			ProtectedServices []string        `json:"protected_services"`
		}
		if json.Unmarshal(raw, &who) != nil {
			return
		}
		s.grantsProt = who.Grants["protected"]
		for _, id := range who.ProtectedServices {
			s.protected[id] = true
		}
	})
}

// refuseProtected is the client-side check before a stop, config edit, or
// image update. The server repeats it; this one avoids the round trip and
// honors GOFORGE_MCP_PROTECTED.
func (s *Server) refuseProtected(ctx context.Context, serviceID, action string) error {
	if serviceID == "" {
		return nil
	}
	s.loadWhoami(ctx)
	if s.protected[serviceID] && !s.grantsProt {
		return fmt.Errorf("service %s is protected; %s needs a token with the protected scope", serviceID, action)
	}
	return nil
}

// containerService finds the service instance behind a container so a stop
// can be checked against the protected list.
func (s *Server) containerService(ctx context.Context, containerID string) (string, error) {
	raw, err := s.api.do(ctx, http.MethodGet, "/containers/"+seg(containerID), nil, nil)
	if err != nil {
		return "", err
	}
	var c struct {
		ServiceInstanceID string `json:"service_instance_id"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", fmt.Errorf("decode container: %w", err)
	}
	return c.ServiceInstanceID, nil
}
