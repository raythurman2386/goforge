package errreport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config is what Apply binds: a DSN (blank means off) and the Sentry
// environment (blank means the Reporter's DefaultEnvironment).
type Config struct {
	DSN         string
	Environment string
}

// flushTimeout bounds how long a replaced client gets to send what it
// already queued.
const flushTimeout = 2 * time.Second

// Reporter owns the Sentry client bound to the process hub. Apply swaps it
// at runtime: sentryhttp clones the hub per request, so new requests use
// the new client at once and requests in flight finish on the old one.
type Reporter struct {
	// Release is the GoForge version; events carry "goforge@<Release>".
	Release string
	// DefaultEnvironment is used when Config.Environment is blank.
	DefaultEnvironment string
	// Redact scrubs secrets from messages, exception values, and
	// breadcrumbs before an event leaves (notify.Redactor.Redact).
	Redact func(string) string
	// Hub is the hub Apply binds; nil means sentry.CurrentHub().
	Hub *sentry.Hub
	// Transport replaces the HTTP transport (tests). Nil uses Sentry's.
	Transport sentry.Transport
	// HTTPTransport is the round tripper for the default transport and
	// SendTest; nil uses http.DefaultTransport.
	HTTPTransport http.RoundTripper
	Logger        *slog.Logger

	mu     sync.Mutex
	client *sentry.Client
	active Config
}

func (r *Reporter) hub() *sentry.Hub {
	if r.Hub != nil {
		return r.Hub
	}
	return sentry.CurrentHub()
}

func (r *Reporter) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// environment returns the Sentry environment for c.
func (r *Reporter) environment(c Config) string {
	if e := strings.TrimSpace(c.Environment); e != "" {
		return e
	}
	if r.DefaultEnvironment != "" {
		return r.DefaultEnvironment
	}
	return "production"
}

// options builds the client options for c. c.DSN must not be blank:
// sentry.NewClient reads SENTRY_DSN from the environment when Dsn is "",
// so a blank DSN is handled by binding no client, never here.
func (r *Reporter) options(c Config, rt http.RoundTripper) sentry.ClientOptions {
	release := r.Release
	if release == "" {
		release = "dev"
	}
	return sentry.ClientOptions{
		Dsn:              c.DSN,
		Release:          "goforge@" + release,
		Environment:      r.environment(c),
		SendDefaultPII:   false,
		TracesSampleRate: 0,
		EnableTracing:    false,
		AttachStacktrace: false,
		BeforeSend:       r.beforeSend,
		// Drop every transaction: tracing is off in v1.
		BeforeSendTransaction: func(*sentry.Event, *sentry.EventHint) *sentry.Event { return nil },
		Transport:             r.Transport,
		HTTPTransport:         rt,
		// One envelope per event, no extra client-report traffic.
		DisableClientReports:   true,
		DisableTelemetryBuffer: true,
	}
}

func (r *Reporter) roundTripper() http.RoundTripper {
	if r.HTTPTransport != nil {
		return r.HTTPTransport
	}
	return http.DefaultTransport
}

// Apply binds a client for c, or no client when c.DSN is blank. The old
// client gets flushTimeout to send what it queued and is then closed. On an
// invalid DSN Apply returns the error and keeps the current client.
func (r *Reporter) Apply(c Config) error {
	c.DSN = strings.TrimSpace(c.DSN)
	r.mu.Lock()
	defer r.mu.Unlock()
	var next *sentry.Client
	if c.DSN != "" {
		cl, err := sentry.NewClient(r.options(c, r.roundTripper()))
		if err != nil {
			return err
		}
		next = cl
	}
	// BindClient(nil) (the SDK's no-op client), never NewClient with an
	// empty Dsn: see options.
	r.hub().BindClient(next)
	old := r.client
	r.client = next
	if next == nil {
		r.active = Config{}
	} else {
		r.active = Config{DSN: c.DSN, Environment: r.environment(c)}
	}
	if old != nil {
		go func() {
			old.Flush(flushTimeout)
			old.Close()
		}()
	}
	if next == nil {
		r.logger().Info("error reporting off")
	} else {
		r.logger().Info("error reporting on", "sentry", DSNHint(c.DSN), "environment", r.active.Environment)
	}
	return nil
}

// Enabled reports whether a client is bound.
func (r *Reporter) Enabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client != nil
}

// Active returns the bound config (blank DSN when off).
func (r *Reporter) Active() Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Flush waits up to timeout for the bound client to send queued events.
// It returns true when off.
func (r *Reporter) Flush(timeout time.Duration) bool {
	r.mu.Lock()
	cl := r.client
	r.mu.Unlock()
	if cl == nil {
		return true
	}
	return cl.Flush(timeout)
}

// ErrNotDelivered means the test event did not reach the Sentry host.
var ErrNotDelivered = errors.New("test event not delivered")

// TestResult reports a SendTest.
type TestResult struct {
	EventID string
	// Status is the HTTP status the ingest answered (0 when unreachable).
	Status int
}

// SendTest sends one "GoForge test event" to c with a throwaway client
// (the bound client is untouched) and waits up to timeout. It returns
// ErrNotDelivered when the ingest was unreachable or answered non-2xx.
func (r *Reporter) SendTest(ctx context.Context, c Config, timeout time.Duration) (TestResult, error) {
	c.DSN = strings.TrimSpace(c.DSN)
	if c.DSN == "" {
		return TestResult{}, errors.New("no DSN")
	}
	rec := &statusRecorder{next: r.roundTripper()}
	opts := r.options(c, rec)
	cl, err := sentry.NewClient(opts)
	if err != nil {
		return TestResult{}, err
	}
	defer cl.Close()
	hub := sentry.NewHub(cl, sentry.NewScope())
	hub.Scope().SetTag("goforge.test", "true")
	id := hub.CaptureMessage("GoForge test event")
	res := TestResult{}
	if id != nil {
		res.EventID = string(*id)
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	flushed := cl.Flush(timeout)
	res.Status = rec.last()
	if r.Transport != nil {
		// Tests: the fake transport has no HTTP status.
		if !flushed {
			return res, ErrNotDelivered
		}
		return res, nil
	}
	if !flushed || res.Status < 200 || res.Status > 299 {
		return res, ErrNotDelivered
	}
	return res, nil
}

// statusRecorder remembers the last ingest status.
type statusRecorder struct {
	next   http.RoundTripper
	mu     sync.Mutex
	status int
}

func (s *statusRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := s.next.RoundTrip(req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status = 0
		return nil, err
	}
	s.status = resp.StatusCode
	return resp, nil
}

func (s *statusRecorder) last() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
