package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/errreport"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
)

const (
	errReportPath = "/settings/error-reporting"
	errReportKey  = "publickeySECRETneverinhtml"
)

type errReportStore struct {
	mu  sync.Mutex
	row *models.ErrorReportingSettings
}

func (s *errReportStore) Get(context.Context) (*models.ErrorReportingSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.row == nil {
		return nil, repositories.ErrNotFound
	}
	c := *s.row
	return &c, nil
}

func (s *errReportStore) Upsert(_ context.Context, r *models.ErrorReportingSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *r
	c.UpdatedAt = time.Now()
	s.row = &c
	return nil
}

// ingest is a loopback fake Sentry that counts envelopes.
type ingest struct {
	*httptest.Server
	mu sync.Mutex
	n  int
}

func newIngest(t *testing.T) *ingest {
	t.Helper()
	in := &ingest{}
	in.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/envelope/") {
			in.mu.Lock()
			in.n++
			in.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(in.Close)
	return in
}

func (in *ingest) count() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.n
}

func (in *ingest) dsn() string {
	return "http://" + errReportKey + "@" + strings.TrimPrefix(in.URL, "http://") + "/1"
}

func errReportHandler(t *testing.T) (*SettingsHandler, *errreport.Reporter, *errReportStore, *fakeAudit) {
	t.Helper()
	// Own hub: these tests never touch sentry.CurrentHub.
	rep := &errreport.Reporter{
		Release: "test", Hub: sentry.NewHub(nil, sentry.NewScope()), Redact: notify.NewRedactor().Redact,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	st, a := &errReportStore{}, &fakeAudit{}
	svc := &errreport.SettingsService{Store: st, Live: rep, DefaultEnvironment: "production"}
	h := (&SettingsHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).WithAudit(a).WithErrorReporting(svc)
	return h, rep, st, a
}

func TestErrorReportSettings_NonAdminForbidden(t *testing.T) {
	h, rep, st, a := errReportHandler(t)
	in := newIngest(t)
	form := url.Values{"dsn": {in.dsn()}}
	for _, tc := range []struct {
		name string
		user *models.User
		want int
	}{{"anonymous", nil, http.StatusUnauthorized}, {"non-admin", plainUser, http.StatusForbidden}} {
		for _, call := range []struct {
			method, path string
			fn           http.HandlerFunc
		}{{http.MethodPut, errReportPath, h.UpdateErrorReportingSettings}, {http.MethodPost, errReportPath + "/test", h.TestErrorReportingSettings}} {
			rr := httptest.NewRecorder()
			call.fn(rr, backupReq(call.method, call.path, form, tc.user))
			if rr.Code != tc.want {
				t.Errorf("%s %s = %d, want %d", tc.name, call.path, rr.Code, tc.want)
			}
		}
	}
	if st.row != nil || in.count() != 0 || len(a.actions) != 0 || rep.Enabled() {
		t.Fatal("a non-admin request saved, sent, applied, or audited")
	}
	if v := h.errReportForm(backupReq(http.MethodGet, "/settings/profile", nil, plainUser), plainUser); v != nil {
		t.Fatal("non-admin gets the card")
	}
}

func TestErrorReportSettings_SaveTestRemoveWithoutRestart(t *testing.T) {
	h, rep, st, a := errReportHandler(t)
	in := newIngest(t)

	// Save: 200, card + toast, key never echoed, audit has names only, live.
	rr := httptest.NewRecorder()
	h.UpdateErrorReportingSettings(rr, backupReq(http.MethodPut, errReportPath, url.Values{"dsn": {in.dsn()}, "environment": {"staging"}}, adminUser))
	if rr.Code != http.StatusOK || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("save %d cache %q: %s", rr.Code, rr.Header().Get("Cache-Control"), rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{`id="error-reporting-settings"`, "Error reporting settings saved", "Error reporting is on (from settings)", strings.TrimPrefix(in.URL, "http://") + "/1", `name="clear_dsn"`} {
		if !strings.Contains(body, want) {
			t.Errorf("save response missing %q", want)
		}
	}
	if strings.Contains(body, errReportKey) {
		t.Fatal("save response leaks the DSN key")
	}
	if st.row == nil || st.row.DSN != in.dsn() || !rep.Enabled() {
		t.Fatalf("stored %+v enabled %v", st.row, rep.Enabled())
	}
	if len(a.actions) != 1 || a.actions[0] != models.AuditActionErrorReportingSettingsUpdate {
		t.Fatalf("audit %v", a.actions)
	}
	if changed, _ := a.details[0]["changed"].([]string); strings.Join(changed, ",") != "dsn,environment" {
		t.Errorf("audit details %v", a.details[0])
	}

	// Send test (blank DSN uses the saved one): exactly one envelope.
	rr = httptest.NewRecorder()
	h.TestErrorReportingSettings(rr, backupReq(http.MethodPost, errReportPath+"/test", url.Values{"environment": {"staging"}}, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Test event sent to") || strings.Contains(rr.Body.String(), errReportKey) {
		t.Fatalf("test %d: %s", rr.Code, rr.Body.String())
	}
	if in.count() != 1 {
		t.Fatalf("ingest got %d envelopes, want 1", in.count())
	}

	// Blank DSN keeps it.
	rr = httptest.NewRecorder()
	h.UpdateErrorReportingSettings(rr, backupReq(http.MethodPut, errReportPath, url.Values{"environment": {"staging"}}, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "No changes to save") || st.row.DSN != in.dsn() || len(a.actions) != 1 {
		t.Fatalf("blank keeps: %d %v", rr.Code, a.actions)
	}

	// Remove: off at once, Send test refuses, ingest stays at 1.
	rr = httptest.NewRecorder()
	h.UpdateErrorReportingSettings(rr, backupReq(http.MethodPut, errReportPath, url.Values{"clear_dsn": {"true"}, "environment": {"staging"}}, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Error reporting is off") || rep.Enabled() || st.row.DSN != "" {
		t.Fatalf("remove %d enabled %v: %s", rr.Code, rep.Enabled(), rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.TestErrorReportingSettings(rr, backupReq(http.MethodPost, errReportPath+"/test", url.Values{}, adminUser))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "error reporting is off") {
		t.Fatalf("test after remove %d: %s", rr.Code, rr.Body.String())
	}
	if in.count() != 1 {
		t.Fatalf("ingest got %d envelopes after remove, want 1", in.count())
	}
}

func TestErrorReportSettings_BadDSNIs400AndNotEchoed(t *testing.T) {
	h, rep, st, a := errReportHandler(t)
	rr := httptest.NewRecorder()
	h.UpdateErrorReportingSettings(rr, backupReq(http.MethodPut, errReportPath, url.Values{"dsn": {"http://" + errReportKey + "@sentry.example.test/1"}}, adminUser))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "DSN must use https://") {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), errReportKey) || st.row != nil || len(a.actions) != 0 || rep.Enabled() {
		t.Fatal("bad DSN echoed, stored, audited, or applied")
	}
}

func TestErrorReport_RenderError500CapturedOnRequestHub(t *testing.T) {
	tr := &sentry.MockTransport{}
	cl, err := sentry.NewClient(sentry.ClientOptions{Dsn: "https://k@sentry.example.test/1", Transport: tr})
	if err != nil {
		t.Fatal(err)
	}
	hub := sentry.NewHub(cl, sentry.NewScope())
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError, http.StatusBadGateway} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req = req.WithContext(sentry.SetHubOnContext(req.Context(), hub))
		RenderError(httptest.NewRecorder(), req, status, "boom")
	}
	if n := len(tr.Events()); n != 2 {
		t.Fatalf("RenderError sent %d events, want 2 (500 and 502 only)", n)
	}
}
