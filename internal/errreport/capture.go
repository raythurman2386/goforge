package errreport

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5"
)

// hubFor returns the request hub sentryhttp put on ctx, or the process hub.
func hubFor(ctx context.Context) *sentry.Hub {
	if ctx != nil {
		if h := sentry.GetHubFromContext(ctx); h != nil {
			return h
		}
	}
	return sentry.CurrentHub()
}

// CapturePanic reports a recovered panic value (from a recover() that does
// not re-panic, such as the API v1 gate or the MCP transport). where tags
// the event. It is a no-op when error reporting is off.
func CapturePanic(ctx context.Context, where string, p any) {
	if p == nil {
		return
	}
	hub := hubFor(ctx)
	if !hub.Client().IsEnabled() {
		return
	}
	hub.WithScope(func(s *sentry.Scope) {
		s.SetTag("goforge.recovered_in", where)
		hub.RecoverWithContext(ctx, p)
	})
}

// CaptureServerError reports a handled 5xx (handlers.RenderError): the
// status, the human message, and the chi route pattern (never the raw
// path, which can hold IDs). It is a no-op when error reporting is off or
// status is below 500.
func CaptureServerError(r *http.Request, status int, message string) {
	if status < http.StatusInternalServerError || r == nil {
		return
	}
	hub := hubFor(r.Context())
	if !hub.Client().IsEnabled() {
		return
	}
	route := ""
	if rc := chi.RouteContext(r.Context()); rc != nil {
		route = rc.RoutePattern()
	}
	if route == "" {
		route = "unknown"
	}
	hub.WithScope(func(s *sentry.Scope) {
		s.SetLevel(sentry.LevelError)
		s.SetTag("http.route", route)
		s.SetTag("http.method", r.Method)
		s.SetTag("http.status_code", strconv.Itoa(status))
		s.SetFingerprint([]string{"render-error", r.Method, route, strconv.Itoa(status)})
		hub.CaptureMessage(fmt.Sprintf("HTTP %d %s %s: %s", status, r.Method, route, message))
	})
}

// DSNHint is scheme://host/project for a DSN, never the key. It returns ""
// when raw does not parse.
func DSNHint(raw string) string {
	d, err := sentry.NewDsn(raw)
	if err != nil {
		return ""
	}
	host := d.GetHost()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if p := d.GetPort(); p != 0 && !(d.GetScheme() == "https" && p == 443) && !(d.GetScheme() == "http" && p == 80) {
		host += ":" + strconv.Itoa(p)
	}
	path := d.GetPath()
	return d.GetScheme() + "://" + host + path + "/" + d.GetProjectID()
}
