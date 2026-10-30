package mcphttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const apiPrefix = "/api/v1"

// maxInnerResponse caps one buffered /api/v1 answer. mcpserver reads at
// most 4 MiB of it.
const maxInnerResponse = 8 << 20

// forwardHeaders are the proxy headers httputil.ClientIP reads. They are
// copied from the /mcp request so the audit row records the same client IP
// /api/v1 would. They only count when RemoteAddr is a trusted proxy, which
// is the /mcp request's own peer.
var forwardHeaders = []string{"X-Forwarded-For", "X-Real-Ip", "Cf-Connecting-Ip", "X-Forwarded-Proto"}

// peer is what the inner request inherits from the /mcp request.
type peer struct {
	remoteAddr string
	host       string
	userAgent  string
	forwarded  http.Header
}

func peerOf(r *http.Request) peer {
	p := peer{remoteAddr: r.RemoteAddr, host: r.Host, forwarded: http.Header{}}
	for _, k := range forwardHeaders {
		for _, v := range r.Header.Values(k) {
			p.forwarded.Add(k, v)
		}
	}
	p.userAgent = "goforge-mcp-http"
	if ua := r.UserAgent(); ua != "" {
		p.userAgent += " (" + ua + ")"
	}
	return p
}

// inProcess is an http.RoundTripper that serves each request with the
// /api/v1 handler directly. No socket, no second trip through the router's
// rate limiter: the /mcp request already passed it.
type inProcess struct {
	api    http.Handler
	peer   peer
	logger *slog.Logger
}

func (t *inProcess) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	// The SDK's context is the /mcp request's, which carries the outer
	// router's chi route context (method POST, pattern /mcp). A chi router
	// reuses a route context it finds, so drop it: without this a GET tool
	// call is routed as POST and answered 405.
	in := req.Clone(context.WithValue(req.Context(), chi.RouteCtxKey, (*chi.Context)(nil)))
	if !strings.HasPrefix(in.URL.Path, apiPrefix+"/") {
		return nil, fmt.Errorf("mcphttp: refusing in-process call outside %s: %s", apiPrefix, in.URL.Path)
	}
	in.URL.Path = strings.TrimPrefix(in.URL.Path, apiPrefix)
	if in.URL.RawPath != "" {
		in.URL.RawPath = strings.TrimPrefix(in.URL.RawPath, apiPrefix)
	}
	in.RequestURI = in.URL.RequestURI()
	in.RemoteAddr = t.peer.remoteAddr
	in.Host = t.peer.host
	for _, k := range forwardHeaders {
		in.Header.Del(k)
		for _, v := range t.peer.forwarded.Values(k) {
			in.Header.Add(k, v)
		}
	}
	in.Header.Set("User-Agent", t.peer.userAgent)
	if in.Body == nil {
		in.Body = http.NoBody
	}

	rec := &recorder{header: http.Header{}}
	defer func() {
		// A panic here would run on an SDK goroutine, outside the router's
		// Recoverer, and take the process down.
		if p := recover(); p != nil {
			t.logger.Error("mcp http: api handler panicked", "path", in.URL.Path, "panic", p)
			resp, err = nil, fmt.Errorf("mcphttp: internal error serving %s", in.URL.Path)
		}
	}()
	t.api.ServeHTTP(rec, in)
	return rec.result(req), nil
}

// recorder buffers one handler answer.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if room := maxInnerResponse - r.body.Len(); room < len(p) {
		if room > 0 {
			r.body.Write(p[:room])
		}
		return 0, fmt.Errorf("mcphttp: response over %d bytes", maxInnerResponse)
	}
	return r.body.Write(p)
}

func (r *recorder) result(req *http.Request) *http.Response {
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        r.header,
		Body:          io.NopCloser(bytes.NewReader(r.body.Bytes())),
		ContentLength: int64(r.body.Len()),
		Request:       req,
	}
}
