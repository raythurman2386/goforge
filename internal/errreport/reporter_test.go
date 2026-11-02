package errreport

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"

	"github.com/raythurman2386/goforge/internal/notify"
)

// These tests bind clients to sentry.CurrentHub() (what sentryhttp clones
// per request), so none of them is parallel.

const testDSN = "https://publickey-SECRET@o1.ingest.sentry.example.test/42"

func newTestReporter(t *testing.T) (*Reporter, *sentry.MockTransport) {
	t.Helper()
	tr := &sentry.MockTransport{}
	r := &Reporter{
		Release: "v9.9.9-test", DefaultEnvironment: "test",
		Redact:    notify.NewRedactor().Redact,
		Transport: tr,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })
	return r, tr
}

// testRouter is the production order: Recoverer, then sentryhttp.
func testRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(chi_middleware.Recoverer)
	r.Use(sentryhttp.New(sentryhttp.Options{Repanic: true}).Handle)
	r.Get("/boom/{id}", func(http.ResponseWriter, *http.Request) {
		panic("connect postgres://goforge:pw-SECRET@db:5432/goforge failed")
	})
	r.Get("/boom-err", func(http.ResponseWriter, *http.Request) {
		panic(fmt.Errorf("dial failed: password=hunter2-SECRET"))
	})
	r.Get("/abort", func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})
	r.Get("/projects/{id}/fail", func(w http.ResponseWriter, r *http.Request) {
		CaptureServerError(r, http.StatusInternalServerError, "Failed to load project")
		w.WriteHeader(http.StatusInternalServerError)
	})
	r.Get("/projects/{id}/missing", func(w http.ResponseWriter, r *http.Request) {
		CaptureServerError(r, http.StatusNotFound, "Not found")
		w.WriteHeader(http.StatusNotFound)
	})
	return r
}

func hit(t *testing.T, h http.Handler, path string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Cookie", "goforge_session=sess-SECRET")
	req.Header.Set("Authorization", "Bearer tok-SECRET-abcdef")
	req.Header.Set("X-CSRF-Token", "csrf-SECRET")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

func assertNoSecrets(t *testing.T, ev *sentry.Event) {
	t.Helper()
	b, err := ev.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, bad := range []string{"SECRET", "hunter2", "Cookie", "cookie", "Authorization", "X-Csrf-Token", "203.0.113.9"} {
		if strings.Contains(s, bad) {
			t.Errorf("event JSON contains %q: %s", bad, s)
		}
	}
}

func TestErrorReport_PanicCapturedRedactedAndStillA500(t *testing.T) {
	rep, tr := newTestReporter(t)
	if err := rep.Apply(Config{DSN: testDSN}); err != nil {
		t.Fatal(err)
	}
	h := testRouter()
	if code := hit(t, h, "/boom/123?token=q-SECRET&x=1"); code != http.StatusInternalServerError {
		t.Fatalf("panic answered %d, want 500 from Recoverer", code)
	}
	evs := tr.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Level != sentry.LevelFatal || !strings.Contains(ev.Message, "postgres://goforge:[redacted]@db:5432/goforge") {
		t.Fatalf("message = %q level %q", ev.Message, ev.Level)
	}
	if ev.Release != "goforge@v9.9.9-test" || ev.Environment != "test" {
		t.Errorf("release %q env %q", ev.Release, ev.Environment)
	}
	if ev.Request == nil || ev.Request.QueryString != "" || strings.Contains(ev.Request.URL, "?") || ev.Request.Data != "" || ev.Request.Cookies != "" {
		t.Errorf("request not scrubbed: %+v", ev.Request)
	}
	for k := range ev.Request.Headers {
		if droppedHeaders[strings.ToLower(k)] {
			t.Errorf("header %s kept", k)
		}
	}
	if ev.User.IPAddress != "" || ev.User.Email != "" {
		t.Errorf("user details sent: %+v", ev.User)
	}
	assertNoSecrets(t, ev)

	// An error panic is an exception; its value is redacted too.
	if code := hit(t, h, "/boom-err"); code != http.StatusInternalServerError {
		t.Fatalf("code %d", code)
	}
	evs = tr.Events()
	if len(evs) != 2 || len(evs[1].Exception) == 0 {
		t.Fatalf("events = %d, want an exception event", len(evs))
	}
	if v := evs[1].Exception[len(evs[1].Exception)-1].Value; !strings.Contains(v, "password=[redacted]") {
		t.Errorf("exception value = %q", v)
	}
	assertNoSecrets(t, evs[1])
}

func TestErrorReport_RenderError500CapturedWithRoutePattern(t *testing.T) {
	rep, tr := newTestReporter(t)
	if err := rep.Apply(Config{DSN: testDSN, Environment: "staging"}); err != nil {
		t.Fatal(err)
	}
	h := testRouter()
	hit(t, h, "/projects/0b9c-uuid/missing")
	if n := len(tr.Events()); n != 0 {
		t.Fatalf("a 404 sent %d events", n)
	}
	hit(t, h, "/projects/0b9c-uuid/fail?token=q-SECRET")
	evs := tr.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Tags["http.route"] != "/projects/{id}/fail" || ev.Tags["http.status_code"] != "500" || ev.Level != sentry.LevelError {
		t.Errorf("tags %v level %q", ev.Tags, ev.Level)
	}
	if ev.Message != "HTTP 500 GET /projects/{id}/fail: Failed to load project" || strings.Contains(ev.Message, "0b9c-uuid") {
		t.Errorf("message %q", ev.Message)
	}
	if ev.Environment != "staging" {
		t.Errorf("environment %q", ev.Environment)
	}
	assertNoSecrets(t, ev)
}

func TestErrorReport_BlankApplySendsNothingWithoutRestart(t *testing.T) {
	rep, tr := newTestReporter(t)
	h := testRouter()
	if err := rep.Apply(Config{DSN: testDSN}); err != nil {
		t.Fatal(err)
	}
	hit(t, h, "/boom/1")
	if len(tr.Events()) != 1 || !rep.Enabled() {
		t.Fatalf("on: events %d", len(tr.Events()))
	}
	if err := rep.Apply(Config{}); err != nil {
		t.Fatal(err)
	}
	if rep.Enabled() || sentry.CurrentHub().Client().IsEnabled() {
		t.Fatal("blank Apply left a client bound")
	}
	if code := hit(t, h, "/boom/2"); code != http.StatusInternalServerError {
		t.Fatalf("off: panic answered %d, want 500", code)
	}
	hit(t, h, "/projects/1/fail")
	if n := len(tr.Events()); n != 1 {
		t.Fatalf("after blank Apply events = %d, want still 1", n)
	}
	// And back on, same process.
	if err := rep.Apply(Config{DSN: testDSN}); err != nil {
		t.Fatal(err)
	}
	hit(t, h, "/boom/3")
	if n := len(tr.Events()); n != 2 {
		t.Fatalf("after re-Apply events = %d, want 2", n)
	}
}

func TestErrorReport_BlankDSNIgnoresSentryDSNEnv(t *testing.T) {
	t.Setenv("SENTRY_DSN", testDSN)
	rep, tr := newTestReporter(t)
	if err := rep.Apply(Config{DSN: "   "}); err != nil {
		t.Fatal(err)
	}
	if rep.Enabled() || sentry.CurrentHub().Client().IsEnabled() {
		t.Fatal("blank DSN bound a client (the SDK read SENTRY_DSN)")
	}
	hit(t, testRouter(), "/boom/1")
	if len(tr.Events()) != 0 {
		t.Fatal("blank DSN sent an event")
	}
}

func TestErrorReport_AbortHandlerDropped(t *testing.T) {
	rep, tr := newTestReporter(t)
	if err := rep.Apply(Config{DSN: testDSN}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }() // Recoverer re-panics ErrAbortHandler.
		hit(t, testRouter(), "/abort")
	}()
	if n := len(tr.Events()); n != 0 {
		t.Fatalf("http.ErrAbortHandler sent %d events", n)
	}
}

func TestErrorReport_CapturePanicFromInnerRecover(t *testing.T) {
	rep, tr := newTestReporter(t)
	CapturePanic(t.Context(), "api_v1.deploy", "off: nothing bound")
	if err := rep.Apply(Config{DSN: testDSN}); err != nil {
		t.Fatal(err)
	}
	CapturePanic(t.Context(), "api_v1.deploy", errors.New("nil map write, token=tk-SECRET"))
	evs := tr.Events()
	if len(evs) != 1 || evs[0].Tags["goforge.recovered_in"] != "api_v1.deploy" {
		t.Fatalf("events %d", len(evs))
	}
	assertNoSecrets(t, evs[0])
}

func TestErrorReport_InvalidDSNKeepsCurrentClient(t *testing.T) {
	rep, _ := newTestReporter(t)
	if err := rep.Apply(Config{DSN: testDSN}); err != nil {
		t.Fatal(err)
	}
	if err := rep.Apply(Config{DSN: "not a dsn"}); err == nil {
		t.Fatal("invalid DSN applied")
	}
	if !rep.Enabled() || rep.Active().DSN != testDSN {
		t.Fatal("invalid DSN dropped the working client")
	}
}

func TestErrorReport_SendTestDeliversOneEnvelope(t *testing.T) {
	var envelopes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		envelopes = append(envelopes, r.URL.Path+"\n"+string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rep := &Reporter{Release: "t", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	dsn := "http://pubkey@" + strings.TrimPrefix(srv.URL, "http://") + "/7"
	res, err := rep.SendTest(t.Context(), Config{DSN: dsn}, 5e9)
	if err != nil || res.Status != http.StatusOK || res.EventID == "" {
		t.Fatalf("SendTest = %+v, %v", res, err)
	}
	if len(envelopes) != 1 || !strings.HasPrefix(envelopes[0], "/api/7/envelope/") || !strings.Contains(envelopes[0], "GoForge test event") {
		t.Fatalf("envelopes = %q", envelopes)
	}
	if rep.Enabled() || sentry.CurrentHub().Client().IsEnabled() {
		t.Fatal("SendTest bound a client")
	}
	// A failing ingest is reported as not delivered.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer bad.Close()
	res, err = rep.SendTest(t.Context(), Config{DSN: "http://pubkey@" + strings.TrimPrefix(bad.URL, "http://") + "/7"}, 5e9)
	if !errors.Is(err, ErrNotDelivered) || res.Status != http.StatusForbidden {
		t.Fatalf("SendTest to 403 = %+v, %v", res, err)
	}
}

func TestErrorReport_DSNHintNeverHasKey(t *testing.T) {
	for raw, want := range map[string]string{
		testDSN:                               "https://o1.ingest.sentry.example.test/42",
		"http://k@127.0.0.1:9000/5":           "http://127.0.0.1:9000/5",
		"https://k@sentry.example.test/x/y/3": "https://sentry.example.test/x/y/3",
		"garbage":                             "",
	} {
		if got := DSNHint(raw); got != want {
			t.Errorf("DSNHint(%q) = %q, want %q", raw, got, want)
		}
	}
}
