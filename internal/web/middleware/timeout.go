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
