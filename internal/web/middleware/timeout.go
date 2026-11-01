package middleware

import (
	"net/http"
	"time"
)

// Timeout returns middleware that applies a write deadline to non-streaming
// HTTP handlers. It wraps the standard http.TimeoutHandler, which cancels
// the request context and returns 503 Service Unavailable if the handler
// does not complete within the given duration.
//
// This middleware should NOT be applied to SSE or other long-lived streaming
// endpoints. Instead, apply it selectively to route groups that serve
// regular request/response traffic.
func Timeout(dt time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, dt, "Service Unavailable")
	}
}

// TimeoutJSON is Timeout for the JSON API and MCP routes. On timeout it
// answers 503 with the API's JSON error body, code "timeout", instead of
// plain text, so API and MCP clients can still parse it. Mutations under
// /api/v1 normally answer their own JSON timeout first and keep running;
// this is the fallback for anything that does not.
func TimeoutJSON(dt time.Duration) func(http.Handler) http.Handler {
	body := `{"error":{"code":"timeout","message":"the request did not finish within ` + dt.String() + `"}}`
	return func(next http.Handler) http.Handler {
		th := http.TimeoutHandler(next, dt, body)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			th.ServeHTTP(&jsonTimeoutWriter{ResponseWriter: w}, r)
		})
	}
}

// jsonTimeoutWriter labels the timeout handler's own 503 as JSON. A handler
// that answered in time already set its Content-Type, which is kept.
type jsonTimeoutWriter struct {
	http.ResponseWriter
}

func (w *jsonTimeoutWriter) WriteHeader(code int) {
	if code == http.StatusServiceUnavailable && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *jsonTimeoutWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
