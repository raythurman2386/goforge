package errreport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/getsentry/sentry-go"
)

// droppedHeaders never leave GoForge in an event (matched case-insensitively).
var droppedHeaders = map[string]bool{
	"cookie": true, "set-cookie": true, "authorization": true, "proxy-authorization": true,
	"x-csrf-token": true, "x-api-key": true, "x-forwarded-for": true, "x-real-ip": true,
	"cf-connecting-ip": true, "forwarded": true,
}

// beforeSend drops client aborts and scrubs every event.
func (r *Reporter) beforeSend(ev *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	if hint != nil {
		if e, ok := hint.RecoveredException.(error); ok && errors.Is(e, http.ErrAbortHandler) {
			return nil
		}
		if hint.OriginalException != nil && errors.Is(hint.OriginalException, http.ErrAbortHandler) {
			return nil
		}
	}
	return Scrub(ev, r.Redact)
}

// Scrub removes request secrets (cookies, auth and CSRF headers, client IP
// headers, body, query string) and user details other than the ID, and runs
// redact over the message, exception values, and breadcrumbs. redact may be
// nil.
func Scrub(ev *sentry.Event, redact func(string) string) *sentry.Event {
	if ev == nil {
		return nil
	}
	if redact == nil {
		redact = func(s string) string { return s }
	}
	if req := ev.Request; req != nil {
		req.Cookies = ""
		req.Data = ""
		req.QueryString = ""
		req.Env = nil
		if i := strings.IndexAny(req.URL, "?#"); i >= 0 {
			req.URL = req.URL[:i]
		}
		for k := range req.Headers {
			if droppedHeaders[strings.ToLower(k)] {
				delete(req.Headers, k)
			}
		}
	}
	ev.User = sentry.User{ID: ev.User.ID}
	if ev.Message != "" {
		ev.Message = redact(ev.Message)
	}
	for i := range ev.Exception {
		ev.Exception[i].Value = redact(ev.Exception[i].Value)
	}
	for _, b := range ev.Breadcrumbs {
		if b == nil {
			continue
		}
		b.Message = redact(b.Message)
		b.Data = nil
	}
	return ev
}
