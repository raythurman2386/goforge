// Package mcphttp serves the GoForge MCP tools over streamable HTTP at /mcp
// on goforge serve.
//
// Each POST is authenticated with a Profile API token (Authorization: Bearer
// only; cookies are ignored), then handed to the go-sdk streamable handler in
// stateless JSON mode. The tools are the ones in internal/mcpserver. Each
// tool call is one request to the /api/v1 handler through an in-process
// http.Client carrying the caller's own token, so scope, ownership, the
// protected list, dry_run, approval_ref, and the audit-before-act rule run
// exactly as they do on /api/v1. Nothing leaves the process and the token is
// never forwarded to another service.
package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/mcpserver"
	"github.com/raythurman2386/goforge/internal/models"
)

// Realm is the WWW-Authenticate realm on /mcp answers.
const Realm = "goforge-mcp"

// innerTimeout bounds one in-process /api/v1 call. The /mcp route itself
// sits behind the router's 15 s write timeout.
const innerTimeout = 15 * time.Second

// writeRefusal is the tool error a read-only token gets for a mutation tool.
const writeRefusal = "insufficient_scope: this token lacks the write scope, so write tools do not run on /mcp. " +
	"Create an MCP token with write in Settings → Profile."

// Deps wires the handler.
type Deps struct {
	// API is the /api/v1 router (apiv1.Handler.Routes()), not mounted: paths
	// arrive without the /api/v1 prefix.
	API    http.Handler
	Tokens auth.TokenLookup
	Users  auth.UserLookup
	Logger *slog.Logger
}

// New returns the /mcp handler: cross-origin protection, then bearer auth,
// then the go-sdk streamable HTTP handler (stateless, JSON responses).
func New(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	h := &handler{deps: d}
	sdk := mcp.NewStreamableHTTPHandler(h.server, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       d.Logger,
	})
	return http.NewCrossOriginProtection().Handler(h.authenticate(sdk))
}

type handler struct {
	deps Deps
}

type callerKey struct{}

// caller is the authenticated request. The SDK asks for the server more than
// once per request, so it is built once here.
type caller struct {
	raw   string
	token *models.APIToken
	peer  peer

	once sync.Once
	srv  *mcp.Server
}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.deps.Tokens == nil || h.deps.Users == nil || h.deps.API == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "MCP over HTTP is not configured")
			return
		}
		_, token, err := auth.AuthenticateBearer(r, h.deps.Tokens, h.deps.Users)
		if err != nil {
			challenge := `Bearer realm="` + Realm + `"`
			msg := "missing bearer token"
			if !errors.Is(err, auth.ErrNoBearerToken) {
				challenge += `, error="invalid_token"`
				msg = "invalid or expired API token"
			}
			w.Header().Set("WWW-Authenticate", challenge)
			writeError(w, http.StatusUnauthorized, "unauthorized", msg)
			return
		}
		if !token.IsMCPCapable() {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+Realm+`", error="insufficient_scope", scope="read write"`)
			msg := "this token cannot use /mcp: create a scoped token (read, or read and write) in Settings → Profile"
			if token.IsFull() {
				msg = "full-scope tokens are not accepted on /mcp: create a scoped MCP token (read, or read and write) in Settings → Profile"
			}
			writeError(w, http.StatusForbidden, "insufficient_scope", msg)
			return
		}
		c := &caller{raw: auth.BearerToken(r), token: token, peer: peerOf(r)}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

// server builds the caller's MCP server: write tools only when the token
// has write; read-only tokens get an insufficient_scope tool error instead.
func (h *handler) server(r *http.Request) *mcp.Server {
	c, ok := r.Context().Value(callerKey{}).(*caller)
	if !ok {
		return nil
	}
	c.once.Do(func() {
		cfg := &mcpserver.Config{
			BaseURL:     "http://goforge.internal",
			Token:       c.raw,
			AllowWrites: c.token.HasScope(models.ScopeWrite),
		}
		if !cfg.AllowWrites {
			cfg.WriteRefusal = writeRefusal
		}
		hc := &http.Client{Timeout: innerTimeout, Transport: &inProcess{api: h.deps.API, peer: c.peer, logger: h.deps.Logger}}
		c.srv = mcpserver.New(cfg, hc).MCP()
	})
	return c.srv
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}}) //nolint:errcheck // client went away
}
