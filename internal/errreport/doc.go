// Package errreport sends GoForge control-plane errors to Sentry when an
// admin saves a DSN in Settings → Profile → Error reporting (or SENTRY_DSN
// is set and no settings row exists). With no DSN it is off: no client is
// bound, nothing is captured, and no traffic leaves the process.
//
// v1 captures HTTP handler panics (sentryhttp after chi's Recoverer, plus
// the recover()s in the API v1 gate and the MCP transport) and handled 5xx
// responses rendered through handlers.RenderError. Tracing, slog capture,
// and background goroutine panics are out of scope.
package errreport
